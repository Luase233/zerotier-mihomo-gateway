package mobile

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClash struct {
	sync.Mutex
	selected  string
	puts      int
	uncertain bool
}

func setup(t *testing.T) (*Server, *fakeClash) {
	t.Helper()
	f := &fakeClash{selected: "first"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.Lock()
		defer f.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/configs":
			reply(w, 200, map[string]string{"mode": "global", "secret": "must-never-reach-client"})
		case r.Method == "GET" && r.URL.Path == "/proxies":
			reply(w, 200, map[string]any{"proxies": map[string]Proxy{"GLOBAL": {Type: "Selector", Now: f.selected, All: []string{"first", "second", "<script>unsafe</script>"}}, "auto": {Type: "URLTest", Now: "first", All: []string{"first", "second"}}}})
		case r.Method == "PUT" && r.URL.Path == "/proxies/GLOBAL":
			var input struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&input)
			f.puts++
			f.selected = input.Name
			w.WriteHeader(204)
		case r.Method == "GET" && r.URL.Path == "/proxies/GLOBAL":
			if f.uncertain {
				w.WriteHeader(503)
			} else {
				reply(w, 200, Proxy{Type: "Selector", Now: f.selected})
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(upstream.Close)
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	cfg := Config{Listen: "127.0.0.1:8787", AllowedClients: []string{"127.0.0.1"}, Token: strings.Repeat("x", 43), Pipe: `\\.\pipe\test`, StateFile: filepath.Join(t.TempDir(), "status.json")}
	s, err := New(cfg, Controller{Client: &http.Client{Transport: transport, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}
func call(s *Server, method, path, body string, alter func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Authorization", "Bearer "+s.config.Token)
	r.Header.Set("Content-Type", "application/json")
	if alter != nil {
		alter(r)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestAccessBoundary(t *testing.T) {
	s, f := setup(t)
	cases := []struct {
		name  string
		alter func(*http.Request)
		want  int
	}{
		{"no token", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"wrong token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }, 401},
		{"other peer", func(r *http.Request) { r.RemoteAddr = "10.0.0.99:50000" }, 403},
		{"forged forwarded IP", func(r *http.Request) { r.RemoteAddr = "10.0.0.99:50000"; r.Header.Set("X-Forwarded-For", "127.0.0.1") }, 403},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.invalid") }, 403},
		{"DNS rebinding host", func(r *http.Request) { r.Host = "attacker.invalid:8787" }, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := call(s, "POST", "/api/select", `{"id":"12345678","group":"GLOBAL","name":"second"}`, tc.alter)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
	if f.puts != 0 {
		t.Fatal("unauthorized command reached Clash")
	}
	if w := call(s, "POST", "/api/restart", `{}`, nil); w.Code != 405 {
		t.Fatalf("unexpected endpoint allowed: %d", w.Code)
	}
}
func TestThinReadAndVerifiedIdempotentWrite(t *testing.T) {
	s, f := setup(t)
	w := call(s, "GET", "/api/state", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "must-never") {
		t.Fatal(w.Body.String())
	}
	body := `{"id":"request-0001","group":"GLOBAL","name":"second"}`
	for i := 0; i < 2; i++ {
		w = call(s, "POST", "/api/select", body, nil)
		var event Event
		if json.Unmarshal(w.Body.Bytes(), &event) != nil || event.Status != "success" || event.Current != "second" {
			t.Fatalf("unverified result: %s", w.Body.String())
		}
	}
	if f.puts != 1 {
		t.Fatal("duplicate command was reapplied")
	}
	b, err := os.ReadFile(s.config.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	_ = json.Unmarshal(b, &state)
	if len(state.Events) != 1 || state.Events[0].Before != "first" || state.Events[0].Client != "127.0.0.1" || state.Events[0].Status != "success" {
		t.Fatalf("invalid desktop audit: %s", b)
	}
	if w = call(s, "POST", "/api/select", `{"id":"request-0001","group":"GLOBAL","name":"first"}`, nil); w.Code != 409 {
		t.Fatal("request ID conflict was accepted")
	}
}
func TestInvalidCommandsCannotWrite(t *testing.T) {
	s, f := setup(t)
	for _, body := range []string{`{"id":"12345678","group":"GLOBAL","name":"missing"}`, `{"id":"12345678","group":"auto","name":"second"}`, `{"id":"12345678","group":"../../configs","name":"second"}`, `{"id":"12345678","group":"GLOBAL","name":"second","extra":1}`, `{"id":"12345678","group":"GLOBAL","name":"second"}{}`, `{"id":"12345678","group":"GLOBAL","name":"` + strings.Repeat("x", 5000) + `"}`} {
		if w := call(s, "POST", "/api/select", body, nil); w.Code != 400 {
			t.Fatalf("accepted invalid command: %d", w.Code)
		}
	}
	if f.puts != 0 {
		t.Fatal("invalid command reached Clash")
	}
}
func TestUnconfirmedAndAuditFailure(t *testing.T) {
	s, f := setup(t)
	f.uncertain = true
	w := call(s, "POST", "/api/select", `{"id":"request-0002","group":"GLOBAL","name":"second"}`, nil)
	var event Event
	_ = json.Unmarshal(w.Body.Bytes(), &event)
	if event.Status != "unconfirmed" {
		t.Fatal("reported success without readback")
	}
	s.config.StateFile = filepath.Join(t.TempDir(), "missing", "status.json")
	w = call(s, "POST", "/api/select", `{"id":"request-0003","group":"GLOBAL","name":"first"}`, nil)
	if w.Code != 503 || f.puts != 1 {
		t.Fatal("command executed without durable pending record")
	}
}
func TestConfigAndStatic(t *testing.T) {
	s, _ := setup(t)
	c := s.config
	c.Listen = "0.0.0.0:8787"
	if c.Validate() == nil {
		t.Fatal("wildcard accepted")
	}
	c = s.config
	c.Pipe = `\\remote\pipe\clash`
	if c.Validate() == nil {
		t.Fatal("remote pipe accepted")
	}
	for _, path := range []string{"/", "/app.js", "/style.css", "/manifest.webmanifest", "/icon.svg", "/apple-touch-icon.png"} {
		w := call(s, "GET", path, "", nil)
		if w.Code != 200 {
			t.Fatalf("asset %s: %d", path, w.Code)
		}
		b, _ := io.ReadAll(w.Result().Body)
		if len(b) == 0 {
			t.Fatal("empty asset")
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
}
