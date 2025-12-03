package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/litescript/ls-torrent-tui/internal/theme"
)

// GetStyles returns current themed styles
func GetStyles() theme.Styles {
	return theme.Current
}

// HealthBar renders a visual health indicator
func HealthBar(health int, width int) string {
	styles := GetStyles()

	filled := (health * width) / 100
	if filled > width {
		filled = width
	}

	var style lipgloss.Style
	switch {
	case health >= 70:
		style = styles.HealthGood
	case health >= 40:
		style = styles.HealthMed
	default:
		style = styles.HealthBad
	}

	bar := style.Render(repeat("█", filled))
	empty := styles.Muted.Render(repeat("░", width-filled))

	return bar + empty
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

// TruncateString truncates a string to max length with ellipsis
func TruncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// PadRight pads a string to a specific width
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + repeat(" ", width-len(s))
}

// PadLeft pads a string on the left to a specific width
func PadLeft(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return repeat(" ", width-len(s)) + s
}

// formatSize formats bytes to human readable size
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatSpeed formats bytes/sec to human readable speed
func formatSpeed(bytesPerSec int64) string {
	if bytesPerSec == 0 {
		return "-"
	}
	return formatSize(bytesPerSec) + "/s"
}

// formatETA formats remaining time (max 7 chars)
func formatETA(amountLeft int64, dlSpeed int64) string {
	if dlSpeed == 0 || amountLeft == 0 {
		if amountLeft == 0 {
			return "-"
		}
		return "∞"
	}

	seconds := amountLeft / dlSpeed
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	if seconds < 86400 {
		hours := seconds / 3600
		mins := (seconds % 3600) / 60
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	if days > 99 {
		return fmt.Sprintf("%dd", days) // Just days for very long ETAs
	}
	return fmt.Sprintf("%dd%dh", days, hours)
}
