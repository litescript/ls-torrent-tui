package theme

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/ini.v1"
)

// Detect attempts to load theme from various sources in priority order
func Detect() Palette {
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultPalette()
	}

	// Priority order (matching OmNote):
	// 1. Omarchy theme
	// 2. Alacritty config
	// 3. Kitty config
	// 4. Foot config
	// 5. Environment overrides
	// 6. Default fallback

	if p, ok := detectOmarchy(home); ok {
		return applyEnvOverrides(p)
	}

	if p, ok := detectAlacritty(home); ok {
		return applyEnvOverrides(p)
	}

	if p, ok := detectKitty(home); ok {
		return applyEnvOverrides(p)
	}

	if p, ok := detectFoot(home); ok {
		return applyEnvOverrides(p)
	}

	return applyEnvOverrides(DefaultPalette())
}

// detectOmarchy reads from ~/.config/omarchy/current/theme/alacritty.toml
func detectOmarchy(home string) (Palette, bool) {
	path := filepath.Join(home, ".config", "omarchy", "current", "theme", "alacritty.toml")
	return parseAlacrittyTOML(path)
}

// detectAlacritty reads from ~/.config/alacritty/alacritty.toml
func detectAlacritty(home string) (Palette, bool) {
	paths := []string{
		filepath.Join(home, ".config", "alacritty", "alacritty.toml"),
		filepath.Join(home, ".alacritty.toml"),
	}

	for _, path := range paths {
		if p, ok := parseAlacrittyTOML(path); ok {
			return p, true
		}
	}
	return Palette{}, false
}

// AlacrittyConfig represents the relevant parts of alacritty.toml
type AlacrittyConfig struct {
	Colors struct {
		Primary struct {
			Background string `toml:"background"`
			Foreground string `toml:"foreground"`
		} `toml:"primary"`
		Selection struct {
			Background string `toml:"background"`
			Text       string `toml:"text"`
		} `toml:"selection"`
		Cursor struct {
			Cursor string `toml:"cursor"`
		} `toml:"cursor"`
	} `toml:"colors"`
}

func parseAlacrittyTOML(path string) (Palette, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Palette{}, false
	}

	var cfg AlacrittyConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Palette{}, false
	}

	// Need at least bg and fg
	if cfg.Colors.Primary.Background == "" || cfg.Colors.Primary.Foreground == "" {
		return Palette{}, false
	}

	p := DefaultPalette()
	p.BG = normalizeHex(cfg.Colors.Primary.Background)
	p.FG = normalizeHex(cfg.Colors.Primary.Foreground)

	// Derive muted from foreground (dimmer)
	p.Muted = dimColor(p.FG, 0.5)

	// Selection background for highlights
	if cfg.Colors.Selection.Background != "" {
		p.AccentBg = normalizeHex(cfg.Colors.Selection.Background)
	} else {
		p.AccentBg = MixColors(p.BG, p.FG, 0.15)
	}

	return p, true
}

// detectKitty reads from ~/.config/kitty/kitty.conf
func detectKitty(home string) (Palette, bool) {
	path := filepath.Join(home, ".config", "kitty", "kitty.conf")
	return parseKittyConf(path)
}

func parseKittyConf(path string) (Palette, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Palette{}, false
	}

	p := DefaultPalette()
	found := false

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key, value := parts[0], parts[1]
		switch key {
		case "background":
			p.BG = normalizeHex(value)
			found = true
		case "foreground":
			p.FG = normalizeHex(value)
			found = true
		case "selection_background":
			p.AccentBg = normalizeHex(value)
		}
	}

	if found {
		p.Muted = dimColor(p.FG, 0.5)
	}

	return p, found
}

// detectFoot reads from ~/.config/foot/foot.ini
func detectFoot(home string) (Palette, bool) {
	path := filepath.Join(home, ".config", "foot", "foot.ini")
	return parseFootINI(path)
}

func parseFootINI(path string) (Palette, bool) {
	cfg, err := ini.Load(path)
	if err != nil {
		return Palette{}, false
	}

	colors := cfg.Section("colors")
	if colors == nil {
		return Palette{}, false
	}

	bg := colors.Key("background").String()
	fg := colors.Key("foreground").String()

	if bg == "" || fg == "" {
		return Palette{}, false
	}

	p := DefaultPalette()
	p.BG = normalizeHex(bg)
	p.FG = normalizeHex(fg)
	p.Muted = dimColor(p.FG, 0.5)

	if sel := colors.Key("selection-background").String(); sel != "" {
		p.AccentBg = normalizeHex(sel)
	} else {
		p.AccentBg = MixColors(p.BG, p.FG, 0.15)
	}

	return p, true
}

// applyEnvOverrides applies TORRENT_TUI_* environment variables
func applyEnvOverrides(p Palette) Palette {
	if v := os.Getenv("TORRENT_TUI_BG"); v != "" {
		p.BG = normalizeHex(v)
	}
	if v := os.Getenv("TORRENT_TUI_FG"); v != "" {
		p.FG = normalizeHex(v)
	}
	if v := os.Getenv("TORRENT_TUI_MUTED"); v != "" {
		p.Muted = normalizeHex(v)
	}
	if v := os.Getenv("TORRENT_TUI_ACCENT"); v != "" {
		p.Accent = normalizeHex(v)
	}
	return p
}

// normalizeHex ensures color is in #RRGGBB format
func normalizeHex(color string) string {
	color = strings.TrimSpace(color)

	// Handle 0xRRGGBB format
	if strings.HasPrefix(color, "0x") || strings.HasPrefix(color, "0X") {
		color = "#" + color[2:]
	}

	// Add # if missing
	if !strings.HasPrefix(color, "#") {
		color = "#" + color
	}

	// Validate hex format
	if matched, _ := regexp.MatchString(`^#[0-9a-fA-F]{6}$`, color); matched {
		return color
	}

	// Handle shorthand #RGB
	if matched, _ := regexp.MatchString(`^#[0-9a-fA-F]{3}$`, color); matched {
		r, g, b := color[1:2], color[2:3], color[3:4]
		return "#" + r + r + g + g + b + b
	}

	return color
}

// dimColor reduces the brightness of a hex color
func dimColor(hex string, factor float64) string {
	hex = normalizeHex(hex)
	if len(hex) != 7 {
		return hex
	}

	r := hexToByte(hex[1:3])
	g := hexToByte(hex[3:5])
	b := hexToByte(hex[5:7])

	r = byte(float64(r) * factor)
	g = byte(float64(g) * factor)
	b = byte(float64(b) * factor)

	return "#" + byteToHex(r) + byteToHex(g) + byteToHex(b)
}

// MixColors blends two colors together
func MixColors(hex1, hex2 string, t float64) string {
	hex1, hex2 = normalizeHex(hex1), normalizeHex(hex2)
	if len(hex1) != 7 || len(hex2) != 7 {
		return hex1
	}

	r1, g1, b1 := hexToByte(hex1[1:3]), hexToByte(hex1[3:5]), hexToByte(hex1[5:7])
	r2, g2, b2 := hexToByte(hex2[1:3]), hexToByte(hex2[3:5]), hexToByte(hex2[5:7])

	r := byte(float64(r1)*(1-t) + float64(r2)*t)
	g := byte(float64(g1)*(1-t) + float64(g2)*t)
	b := byte(float64(b1)*(1-t) + float64(b2)*t)

	return "#" + byteToHex(r) + byteToHex(g) + byteToHex(b)
}

func hexToByte(s string) byte {
	var v byte
	for _, c := range strings.ToLower(s) {
		v *= 16
		if c >= '0' && c <= '9' {
			v += byte(c - '0')
		} else if c >= 'a' && c <= 'f' {
			v += byte(c - 'a' + 10)
		}
	}
	return v
}

func byteToHex(b byte) string {
	const hex = "0123456789abcdef"
	return string([]byte{hex[b>>4], hex[b&0x0f]})
}
