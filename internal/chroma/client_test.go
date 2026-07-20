package chroma

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRGBToColorRef(t *testing.T) {
	got := rgbToColorRef(0x123456)
	if got != 0x563412 {
		t.Fatalf("got %#x, want %#x", got, uint32(0x563412))
	}
}

func TestCustomKeyEffectOnlyLightsEnter(t *testing.T) {
	effect := customKeyEffect(0x3B82F6)
	if effect.Effect != "CHROMA_CUSTOM_KEY" {
		t.Fatalf("unexpected effect: %s", effect.Effect)
	}
	nonzero := 0
	for r := range effect.Param.Key {
		for c, v := range effect.Param.Key[r] {
			if v != 0 {
				nonzero++
				if r != enterRow || c != enterCol {
					t.Fatalf("unexpected lit key at %d,%d", r, c)
				}
			}
		}
	}
	if nonzero != 1 {
		t.Fatalf("got %d lit keys, want 1", nonzero)
	}
}

func TestNormalizeLocalhost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"port path and query", "http://localhost:1234/chromasdk/session?token=a#fragment", "http://127.0.0.1:1234/chromasdk/session?token=a#fragment"},
		{"case insensitive", "http://LOCALHOST:54235/razer/chromasdk", "http://127.0.0.1:54235/razer/chromasdk"},
		{"IPv4 unchanged", "http://127.0.0.1:1234/a", "http://127.0.0.1:1234/a"},
		{"IPv6 unchanged", "http://[::1]:1234/a", "http://[::1]:1234/a"},
		{"remote unchanged", "https://chromasdk.io:54236/a", "https://chromasdk.io:54236/a"},
		{"broken unchanged", "://not a URI", "://not a URI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLocalhost(tt.in); got != tt.want {
				t.Fatalf("normalizeLocalhost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLocalhostSessionUsesIPv4ForPutAndDelete(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	var server *httptest.Server
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path+" host="+r.Host)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			u, _ := url.Parse(serverURLFromRequest(r))
			fmt.Fprintf(w, `{"sessionid":1,"uri":"http://localhost:%s/session"}`, u.Port())
			return
		}
		fmt.Fprint(w, `{"result":0}`)
	}))
	defer server.Close()

	baseURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/razer/chromasdk"
	c := newClient(baseURL, server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.SetEnter(ctx, 0x123456); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	got := strings.Join(requests, ", ")
	mu.Unlock()
	serverPort := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	want := "POST /razer/chromasdk host=localhost:" + serverPort +
		", PUT /session/keyboard host=localhost:" + serverPort +
		", DELETE /session host=localhost:" + serverPort
	if got != want {
		t.Fatalf("requests = %q, want %q", got, want)
	}
}

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func serverURLFromRequest(r *http.Request) string {
	return "http://" + r.Host
}

func TestErrorClassification(t *testing.T) {
	t.Run("client limit", func(t *testing.T) {
		server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"result":5}`)
		}))
		defer server.Close()
		c := newClient(server.URL, server.Client())
		err := c.EnsureConnected(context.Background())
		if got := ErrorCode(err); got != ErrorClientLimit {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, ErrorClientLimit)
		}
	})

	t.Run("SDK unavailable", func(t *testing.T) {
		c := newClient("http://127.0.0.1:1", &http.Client{Timeout: 100 * time.Millisecond})
		err := c.EnsureConnected(context.Background())
		if got := ErrorCode(err); got != ErrorUnavailable {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, ErrorUnavailable)
		}
	})

	t.Run("session unreachable", func(t *testing.T) {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		unreachable := listener.Addr().String()
		_ = listener.Close()
		server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"sessionid":1,"uri":"http://%s/session"}`, unreachable)
		}))
		defer server.Close()
		c := newClient(server.URL, server.Client())
		err = c.SetEnter(context.Background(), 0)
		if got := ErrorCode(err); got != ErrorSessionUnreachable {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, ErrorSessionUnreachable)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestNewSessionWaitsForListenerWithoutAllocatingAnotherSession(t *testing.T) {
	var posts atomic.Int32
	var puts atomic.Int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPost:
			posts.Add(1)
			return jsonResponse(`{"sessionid":1,"uri":"http://localhost:12345/chromasdk"}`), nil
		case http.MethodPut:
			if puts.Add(1) < 3 {
				return nil, fmt.Errorf("listener not ready")
			}
			return jsonResponse(`{"result":0}`), nil
		default:
			return nil, fmt.Errorf("unexpected method %s", req.Method)
		}
	})
	c := newClient("http://localhost:54235/razer/chromasdk", &http.Client{Transport: transport})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.SetEnter(ctx, 0x123456); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatalf("created %d sessions, want 1", posts.Load())
	}
	if puts.Load() != 3 {
		t.Fatalf("attempted %d PUTs, want 3", puts.Load())
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
