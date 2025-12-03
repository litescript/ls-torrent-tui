// Package qbit provides a client for the qBittorrent Web API.
// It handles authentication, torrent management (add, pause, resume, delete),
// and status monitoring for active downloads.
package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// Client interfaces with qBittorrent Web API
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	loggedIn   bool
}

// TorrentInfo represents a torrent in qBittorrent
type TorrentInfo struct {
	Hash           string  `json:"hash"`
	Name           string  `json:"name"`
	Size           int64   `json:"size"`
	Progress       float64 `json:"progress"`
	DLSpeed        int64   `json:"dlspeed"`
	UPSpeed        int64   `json:"upspeed"`
	NumSeeds       int     `json:"num_seeds"`
	NumLeechers    int     `json:"num_leechs"`
	State          string  `json:"state"`
	SavePath       string  `json:"save_path"`
	AddedOn        int64   `json:"added_on"`
	CompletionOn   int64   `json:"completion_on"`
	AmountLeft     int64   `json:"amount_left"`
	DownloadedEver int64   `json:"downloaded"`
	UploadedEver   int64   `json:"uploaded"`
}

// NewClient creates a new qBittorrent API client
func NewClient(host string, port int, username, password string) *Client {
	jar, _ := cookiejar.New(nil)

	return &Client{
		baseURL:  fmt.Sprintf("http://%s:%d", host, port),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

// Login authenticates with the qBittorrent API
func (c *Client) Login(ctx context.Context) error {
	data := url.Values{}
	data.Set("username", c.username)
	data.Set("password", c.password)

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v2/auth/login", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to qBittorrent: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Ok." {
		return fmt.Errorf("login failed: %s", string(body))
	}

	c.loggedIn = true
	return nil
}

// IsConnected checks if we can reach qBittorrent
func (c *Client) IsConnected(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v2/app/version", nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// GetVersion returns the qBittorrent version
func (c *Client) GetVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v2/app/version", nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

// AddMagnet adds a torrent via magnet link
func (c *Client) AddMagnet(ctx context.Context, magnet string, savePath string) error {
	if !c.loggedIn {
		if err := c.Login(ctx); err != nil {
			return err
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	_ = writer.WriteField("urls", magnet)
	if savePath != "" {
		_ = writer.WriteField("savepath", savePath)
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v2/torrents/add", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add torrent: %s", string(respBody))
	}

	return nil
}

// GetTorrents returns list of torrents
func (c *Client) GetTorrents(ctx context.Context) ([]TorrentInfo, error) {
	if !c.loggedIn {
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v2/torrents/info", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var torrents []TorrentInfo
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}

	return torrents, nil
}

// Pause pauses a torrent
func (c *Client) Pause(ctx context.Context, hash string) error {
	return c.torrentAction(ctx, "pause", hash)
}

// Resume resumes a torrent
func (c *Client) Resume(ctx context.Context, hash string) error {
	return c.torrentAction(ctx, "resume", hash)
}

// Delete removes a torrent (optionally with files)
func (c *Client) Delete(ctx context.Context, hash string, deleteFiles bool) error {
	if !c.loggedIn {
		if err := c.Login(ctx); err != nil {
			return err
		}
	}

	data := url.Values{}
	data.Set("hashes", hash)
	if deleteFiles {
		data.Set("deleteFiles", "true")
	} else {
		data.Set("deleteFiles", "false")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v2/torrents/delete", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *Client) torrentAction(ctx context.Context, action, hash string) error {
	if !c.loggedIn {
		if err := c.Login(ctx); err != nil {
			return err
		}
	}

	data := url.Values{}
	data.Set("hashes", hash)

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v2/torrents/"+action, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
