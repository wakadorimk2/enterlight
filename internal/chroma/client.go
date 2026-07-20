package chroma

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const baseURL = "http://localhost:54235/razer/chromasdk"

const (
	ErrorSessionUnreachable = "chroma_session_unreachable"
	ErrorClientLimit        = "chroma_client_limit"
	ErrorUnavailable        = "chroma_unavailable"
	sessionStartupGrace     = 2500 * time.Millisecond
	sessionStartupPoll      = 100 * time.Millisecond
)

type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func ErrorCode(err error) string {
	var chromaErr *Error
	if errors.As(err, &chromaErr) {
		return chromaErr.Code
	}
	return ErrorUnavailable
}

// Enter is RZKEY_ENTER (row 3, column 14) in Razer's generic 6x22 layout.
const (
	enterRow = 3
	enterCol = 14
	keyMask  = 0x01000000
)

type Client struct {
	http         *http.Client
	baseURL      string
	baseHTTPHost string
	mu           sync.Mutex
	uri          string
	uriHTTPHost  string
	lastUse      time.Time
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
	return newClient(baseURL, &http.Client{Timeout: 3 * time.Second})
}

func newClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		http:         httpClient,
		baseURL:      normalizeLocalhost(baseURL),
		baseHTTPHost: localhostHTTPHost(baseURL),
	}
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
	if err := c.doJSONLocked(ctx, http.MethodPost, c.baseURL, c.baseHTTPHost, payload, &out); err != nil {
		return classifiedError(ErrorUnavailable, "initialize Razer Chroma SDK", err)
	}
	if out.URI == "" {
		code := ErrorUnavailable
		// Access denied and single-instance are the errors returned by known SDK
		// versions when no additional REST client slot can be allocated.
		if out.Result == 5 || out.Result == 1152 {
			code = ErrorClientLimit
		}
		return classifiedError(code, "initialize Razer Chroma SDK", fmt.Errorf("no session URI returned (result=%d)", out.Result))
	}
	rawURI := strings.TrimRight(out.URI, "/")
	c.uri = normalizeLocalhost(rawURI)
	c.uriHTTPHost = localhostHTTPHost(rawURI)
	c.lastUse = time.Now()
	return nil
}

// SetEnter sets a CHROMA_CUSTOM_KEY effect with only the Enter key lit.
func (c *Client) SetEnter(ctx context.Context, rgb uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	newSession := c.uri == ""
	if err := c.ensureConnectedLocked(ctx); err != nil {
		return err
	}
	payload := customKeyEffect(rgb)
	var out resultResponse
	err := c.doJSONLocked(ctx, http.MethodPut, c.uri+"/keyboard", c.uriHTTPHost, payload, &out)
	if err != nil && newSession {
		// Some SDK versions return the session URI before the child HTTP listener
		// is ready. Retry the same session briefly; never allocate another one.
		until := time.Now().Add(sessionStartupGrace)
		for err != nil && time.Now().Before(until) {
			timer := time.NewTimer(sessionStartupPoll)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
				until = time.Time{}
			case <-timer.C:
				out = resultResponse{}
				err = c.doJSONLocked(ctx, http.MethodPut, c.uri+"/keyboard", c.uriHTTPHost, payload, &out)
			}
		}
	}
	if err != nil {
		c.uri = ""
		c.uriHTTPHost = ""
		return classifiedError(ErrorSessionUnreachable, "set Enter key effect", err)
	}
	if out.Result != 0 {
		return classifiedError(ErrorUnavailable, "set Enter key effect", fmt.Errorf("Chroma result=%d", out.Result))
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
	if err := c.doJSONLocked(ctx, http.MethodPut, c.uri+"/heartbeat", c.uriHTTPHost, map[string]any{}, &out); err != nil {
		c.uri = ""
		c.uriHTTPHost = ""
		return classifiedError(ErrorSessionUnreachable, "Chroma heartbeat", err)
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
	req.Host = c.uriHTTPHost
	resp, err := c.http.Do(req)
	c.uri = ""
	c.uriHTTPHost = ""
	if err != nil {
		return classifiedError(ErrorSessionUnreachable, "close Chroma session", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("close Chroma session: HTTP %s", resp.Status)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return "", err
	}
	req.Host = c.baseHTTPHost
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

func classifiedError(code, operation string, err error) error {
	return &Error{Code: code, Err: fmt.Errorf("%s: %w", operation, err)}
}

// normalizeLocalhost forces only the localhost hostname to IPv4. Chroma REST
// sessions listen on IPv4 on affected SDK versions even when Windows resolves
// localhost to ::1 first.
func normalizeLocalhost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Hostname(), "localhost") {
		return raw
	}
	port := u.Port()
	u.Host = "127.0.0.1"
	if port != "" {
		u.Host += ":" + port
	}
	return u.String()
}

// localhostHTTPHost preserves the HTTP.sys URL registration while the request
// itself connects to the normalized IPv4 address.
func localhostHTTPHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "localhost") {
		return ""
	}
	if port := u.Port(); port != "" {
		return "localhost:" + port
	}
	return "localhost"
}

func (c *Client) doJSONLocked(ctx context.Context, method, url, httpHost string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Host = httpHost
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
