package mobile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

//go:embed web/*
var assets embed.FS

type Config struct {
	Listen         string   `json:"listen"`
	AllowedClients []string `json:"allowed_clients"`
	Token          string   `json:"token"`
	Pipe           string   `json:"pipe"`
	StateFile      string   `json:"state_file"`
}

func (c Config) Validate() error {
	addr, err := netip.ParseAddrPort(c.Listen)
	if err != nil || !addr.Addr().Is4() || (!addr.Addr().IsPrivate() && !addr.Addr().IsLoopback()) || addr.Port() == 0 {
		return errors.New("listen must be an explicit private or loopback IPv4 address and port")
	}
	if len(c.Token) < 40 || len(c.Token) > 128 {
		return errors.New("use a random access token of at least 40 characters")
	}
	if len(c.AllowedClients) == 0 {
		return errors.New("allowed_clients is required")
	}
	for _, ip := range c.AllowedClients {
		a, e := netip.ParseAddr(ip)
		if e != nil || !a.Is4() {
			return errors.New("allowed_clients must contain exact IPv4 addresses")
		}
	}
	if !strings.HasPrefix(c.Pipe, `\\.\pipe\`) || strings.ContainsAny(strings.TrimPrefix(c.Pipe, `\\.\pipe\`), `\/`) {
		return errors.New("only a local Windows named pipe is supported")
	}
	if !filepath.IsAbs(c.StateFile) {
		return errors.New("state_file must be an absolute path")
	}
	return nil
}

type Proxy struct {
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}
type Group struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Now        string   `json:"now"`
	All        []string `json:"all,omitempty"`
	Selectable bool     `json:"selectable"`
}
type Event struct {
	ID      string `json:"id"`
	Time    string `json:"time"`
	Client  string `json:"client"`
	Group   string `json:"group"`
	Target  string `json:"target"`
	Before  string `json:"before"`
	Current string `json:"current"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type State struct {
	Updated string  `json:"updated"`
	Online  bool    `json:"online"`
	Mode    string  `json:"mode"`
	Error   string  `json:"error,omitempty"`
	Groups  []Group `json:"groups"`
	Events  []Event `json:"events"`
}
type Controller struct{ Client *http.Client }

func (c Controller) request(ctx context.Context, method, path string, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://mihomo"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Client.Do(req)
	if err != nil {
		return errors.New("Clash controller is unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Clash returned HTTP %d", res.StatusCode)
	}
	if out != nil {
		b, e := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024+1))
		if e != nil || len(b) > 4*1024*1024 {
			return errors.New("invalid Clash response size")
		}
		return json.Unmarshal(b, out)
	}
	return nil
}
func (c Controller) Read(ctx context.Context) (State, error) {
	var config struct {
		Mode string `json:"mode"`
	}
	var proxies struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := c.request(ctx, "GET", "/configs", nil, &config); err != nil {
		return State{}, err
	}
	if err := c.request(ctx, "GET", "/proxies", nil, &proxies); err != nil {
		return State{}, err
	}
	s := State{Online: true, Mode: config.Mode, Groups: []Group{}}
	for name, p := range proxies.Proxies {
		if len(p.All) > 0 {
			s.Groups = append(s.Groups, Group{Name: name, Type: p.Type, Now: p.Now, All: p.All, Selectable: p.Type == "Selector"})
		}
	}
	sort.Slice(s.Groups, func(i, j int) bool {
		if s.Groups[i].Name == "GLOBAL" {
			return true
		}
		if s.Groups[j].Name == "GLOBAL" {
			return false
		}
		return s.Groups[i].Name < s.Groups[j].Name
	})
	return s, nil
}

type Server struct {
	config     Config
	controller Controller
	handler    http.Handler
	mu         sync.Mutex
	state      State
	events     []Event
	limiter    *rate.Limiter
}

func New(c Config, controller Controller) (*Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	s := &Server{config: c, controller: controller, events: []Event{}, limiter: rate.NewLimiter(5, 20)}
	if b, err := os.ReadFile(c.StateFile); err == nil && len(b) < 131072 {
		var old State
		if json.Unmarshal(b, &old) == nil {
			for _, e := range old.Events {
				if e.Status == "pending" {
					e.Status = "unconfirmed"
					e.Message = "Service restarted; check the current selection"
				}
				s.events = append(s.events, e)
			}
			if len(s.events) > 20 {
				s.events = s.events[len(s.events)-20:]
			}
		}
	}
	static, _ := fs.Sub(assets, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("POST /api/select", s.selectProxy)
	files := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		switch r.URL.Path {
		case "/", "/app.js", "/style.css", "/manifest.webmanifest", "/icon.svg", "/apple-touch-icon.png":
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	s.handler = s.protect(mux)
	return s, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "Forbidden", 403)
			return
		}
		allowed := false
		for _, ip := range s.config.AllowedClients {
			if ip == host {
				allowed = true
			}
		}
		if !allowed || r.Host != s.config.Listen {
			http.Error(w, "Forbidden", 403)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+s.config.Listen {
			http.Error(w, "Origin forbidden", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			expected := sha256.Sum256([]byte("Bearer " + s.config.Token))
			got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
			if subtle.ConstantTimeCompare(expected[:], got[:]) != 1 {
				http.Error(w, "Unauthorized", 401)
				return
			}
			if !s.limiter.Allow() {
				http.Error(w, "Too many requests", 429)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) refresh(ctx context.Context) {
	state, err := s.controller.Read(ctx)
	state.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		state.Online = false
		state.Error = err.Error()
		state.Groups = []Group{}
	}
	state.Events = append([]Event{}, s.events...)
	s.state = state
}
func (s *Server) persist() error {
	state := s.state
	state.Events = append([]Event{}, s.events...)
	state.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	state.Groups = append([]Group{}, state.Groups...)
	for i := range state.Groups {
		state.Groups[i].All = nil
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := s.config.StateFile
	f, err := os.CreateTemp(filepath.Dir(path), "mobile-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (s *Server) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh(ctx)
	return s.persist()
}
func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh(r.Context())
	if err := s.persist(); err != nil {
		reply(w, 503, map[string]string{"error": "Cannot synchronize desktop state"})
		return
	}
	reply(w, 200, s.state)
}

var requestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func (s *Server) selectProxy(w http.ResponseWriter, r *http.Request) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "JSON required", 415)
		return
	}
	var input struct {
		ID    string `json:"id"`
		Group string `json:"group"`
		Name  string `json:"name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(&struct{}{}) != io.EOF || !requestID.MatchString(input.ID) || len(input.Group) > 512 || len(input.Name) > 512 {
		http.Error(w, "Invalid request", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.ID == input.ID {
			if e.Group != input.Group || e.Target != input.Name {
				http.Error(w, "Request ID conflict", 409)
				return
			}
			reply(w, 200, e)
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.refresh(ctx)
	if !s.state.Online {
		reply(w, 503, map[string]string{"error": "Clash is unavailable"})
		return
	}
	var group *Group
	for i := range s.state.Groups {
		if s.state.Groups[i].Name == input.Group {
			group = &s.state.Groups[i]
			break
		}
	}
	valid := false
	if group != nil && group.Selectable {
		for _, name := range group.All {
			if name == input.Name {
				valid = true
			}
		}
	}
	if !valid {
		reply(w, 400, map[string]string{"error": "Choose an existing node in a manual selector group"})
		return
	}
	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	event := Event{ID: input.ID, Time: time.Now().UTC().Format(time.RFC3339Nano), Client: client, Group: input.Group, Target: input.Name, Before: group.Now, Status: "pending", Message: "Applying selection"}
	s.events = append(s.events, event)
	if len(s.events) > 20 {
		s.events = s.events[len(s.events)-20:]
	}
	if err := s.persist(); err != nil {
		s.events = s.events[:len(s.events)-1]
		reply(w, 503, map[string]string{"error": "Audit state unavailable; command not executed"})
		return
	}
	err := s.controller.request(ctx, "PUT", "/proxies/"+url.PathEscape(input.Group), map[string]string{"name": input.Name}, nil)
	event.Status = "unconfirmed"
	event.Message = "Selection not confirmed; refresh before retrying"
	if err == nil {
		var actual Proxy
		if e := s.controller.request(ctx, "GET", "/proxies/"+url.PathEscape(input.Group), nil, &actual); e == nil {
			event.Current = actual.Now
			if actual.Now == input.Name {
				event.Status = "success"
				event.Message = "Clash confirmed the selected node"
			}
		}
	}
	s.events[len(s.events)-1] = event
	s.refresh(ctx)
	if e := s.persist(); e != nil {
		reply(w, 503, map[string]string{"error": "Command sent, but desktop synchronization failed; check current selection"})
		return
	}
	reply(w, 200, event)
}
