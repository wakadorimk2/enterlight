package chroma

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const baseURL = "http://localhost:54235/razer/chromasdk"

// Enter is RZKEY_ENTER (row 3, column 14) in Razer's generic 6x22 layout.
const (
	enterRow = 3
	enterCol = 14
	keyMask  = 0x01000000
)

type Client struct {
	http    *http.Client
	mu      sync.Mutex
	uri     string
	lastUse time.Time
}

type initResponse struct {
	SessionID int    `json:"sessionid"`
	URI       string `json:"uri"`
	Result    int    `json:"result"`
}

type resultResponse struct {
	Result int `json:"result"`
}

type appInfo struct {
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Author           author   `json:"author"`
	DevicesSupported []string `json:"device_supported"`
	Category         string   `json:"category"`
}

type author struct {
	Name    string `json:"name"`
	Contact string `json:"contact"`
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 3 * time.Second}}
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uri != ""
}

func (c *Client) EnsureConnected(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensureConnectedLocked(ctx)
}

func (c *Client) ensureConnectedLocked(ctx context.Context) error {
	if c.uri != "" {
		return nil
	}
	payload := appInfo{
		Title:            "Enterlight",
		Description:      "A tiny status light for coding agents, rendered on the Enter key.",
		Author:           author{Name: "wakadorimk2", Contact: "https://github.com/wakadorimk2/enterlight"},
		DevicesSupported: []string{"keyboard"},
		Category:         "application",
	}
	var out initResponse
	if err := c.doJSONLocked(ctx, http.MethodPost, baseURL, payload, &out); err != nil {
		return fmt.Errorf("initialize Razer Chroma SDK: %w", err)
	}
	if out.URI == "" {
		return fmt.Errorf("initialize Razer Chroma SDK: no session URI returned (result=%d)", out.Result)
	}
	c.uri = strings.TrimRight(out.URI, "/")
	c.lastUse = time.Now()
	return nil
}

// SetEnter sets a CHROMA_CUSTOM_KEY effect with only the Enter key lit.
func (c *Client) SetEnter(ctx context.Context, rgb uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConnectedLocked(ctx); err != nil {
		return err
	}
	payload := customKeyEffect(rgb)
	var out resultResponse
	if err := c.doJSONLocked(ctx, http.MethodPut, c.uri+"/keyboard", payload, &out); err != nil {
		c.uri = ""
		return fmt.Errorf("set Enter key effect: %w", err)
	}
	if out.Result != 0 {
		return fmt.Errorf("set Enter key effect: Chroma result=%d", out.Result)
	}
	c.lastUse = time.Now()
	return nil
}

func (c *Client) Heartbeat(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uri == "" {
		return errors.New("not connected")
	}
	var out resultResponse
	if err := c.doJSONLocked(ctx, http.MethodPut, c.uri+"/heartbeat", map[string]any{}, &out); err != nil {
		c.uri = ""
		return fmt.Errorf("Chroma heartbeat: %w", err)
	}
	c.lastUse = time.Now()
	return nil
}

// Close releases the Chroma session so Synapse can restore the user's normal profile.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uri == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.uri, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	c.uri = ""
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("close Chroma session: HTTP %s", resp.Status)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return strings.TrimSpace(string(body)), nil
}

func (c *Client) doJSONLocked(ctx context.Context, method, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if len(bytes.TrimSpace(data)) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response %q: %w", string(data), err)
	}
	return nil
}

type effectPayload struct {
	Effect string          `json:"effect"`
	Param  customKeyParams `json:"param"`
}

type customKeyParams struct {
	Color [][]uint32 `json:"color"`
	Key   [][]uint32 `json:"key"`
}

func customKeyEffect(rgb uint32) effectPayload {
	color := zeroMatrix(6, 22)
	key := zeroMatrix(6, 22)
	key[enterRow][enterCol] = keyMask | rgbToColorRef(rgb)
	return effectPayload{
		Effect: "CHROMA_CUSTOM_KEY",
		Param:  customKeyParams{Color: color, Key: key},
	}
}

func zeroMatrix(rows, cols int) [][]uint32 {
	m := make([][]uint32, rows)
	for i := range m {
		m[i] = make([]uint32, cols)
	}
	return m
}

// rgbToColorRef converts 0xRRGGBB to Windows COLORREF (0x00BBGGRR).
func rgbToColorRef(rgb uint32) uint32 {
	r := (rgb >> 16) & 0xff
	g := (rgb >> 8) & 0xff
	b := rgb & 0xff
	return r | (g << 8) | (b << 16)
}
