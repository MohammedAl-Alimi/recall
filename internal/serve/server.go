// Package serve runs the local web dashboard behind 'recall serve'.
//
// The server binds to loopback only, guards every request with a random
// per-start token and serves one embedded HTML page that talks to a small
// JSON API. All App access is serialised through one mutex because App is
// not safe for concurrent use.
package serve

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/app"
)

//go:embed web/index.html
var webFS embed.FS

// DefaultAddr is the listen address when Options.Addr is empty.
const DefaultAddr = "127.0.0.1:4747"

// TokenFile is the file under RecallDir that keeps the dashboard token
// between starts so a bookmarked URL keeps working.
const TokenFile = "serve.token"

// Refresh intervals for GET /api/sessions and the SSE ticker.
const (
	liveStaleAfter = 10 * time.Second
	loadStaleAfter = 60 * time.Second
	eventInterval  = 3 * time.Second
	heartbeat      = 15 * time.Second
)

// tokenRe matches the 32 hex character token format.
var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Options configures a Server.
type Options struct {
	// Addr is the listen address. Only loopback hosts are accepted.
	// Empty means DefaultAddr.
	Addr string
	// Token is the access token. Empty means: reuse RecallDir/serve.token
	// when it holds a valid token, otherwise generate one and store it.
	Token string
	// OpenBrowser is recorded for the caller; the server itself never opens
	// a browser (cmd/recall does, so RECALL_DRY_RUN can be honored there).
	OpenBrowser bool
}

// Server is the dashboard HTTP server.
type Server struct {
	app   *app.App
	opts  Options
	token string
	mux   *http.ServeMux
	page  []byte

	// mu serialises every call into app.
	mu       sync.Mutex
	lastLoad time.Time
	lastLive time.Time
	loaded   bool

	// runMu serialises launch.Run so two osascripts never race for the
	// front window.
	runMu sync.Mutex

	hub *hub

	// addr is the bound address once ListenAndServe has started.
	addrMu sync.Mutex
	addr   string
}

// New builds a Server for a. It resolves the token (see Options.Token) and
// validates the address; both errors surface from ListenAndServe so New
// itself never fails.
func New(a *app.App, opts Options) *Server {
	if opts.Addr == "" {
		opts.Addr = DefaultAddr
	}
	s := &Server{app: a, opts: opts, hub: newHub()}
	page, err := webFS.ReadFile("web/index.html")
	if err == nil {
		s.page = page
	}
	s.token = opts.Token
	if s.token == "" && a != nil {
		s.token = loadOrCreateToken(a.Paths.RecallDir)
	}
	if s.token == "" {
		s.token = randomToken()
	}
	s.routes()
	return s
}

// Token returns the access token.
func (s *Server) Token() string { return s.token }

// Addr returns the configured listen address (or the bound one once the
// server is listening).
func (s *Server) Addr() string {
	s.addrMu.Lock()
	defer s.addrMu.Unlock()
	if s.addr != "" {
		return s.addr
	}
	return s.opts.Addr
}

// URL returns the dashboard URL including the token.
func (s *Server) URL() string {
	return "http://" + s.Addr() + "/?t=" + s.token
}

// Handler returns the HTTP handler, for tests and embedding.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe binds the loopback address and serves until ctx is done.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.app == nil {
		return errors.New("serve: no app")
	}
	if err := checkLoopback(s.opts.Addr); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("serve: listen %s: %w", s.opts.Addr, err)
	}
	s.addrMu.Lock()
	s.addr = ln.Addr().String()
	s.addrMu.Unlock()

	srv := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	tickCtx, stopTick := context.WithCancel(ctx)
	go s.ticker(tickCtx)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		stopTick()
		s.hub.closeAll()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errc:
		stopTick()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// ticker refreshes live state every eventInterval while at least one SSE
// client is connected and tells them to refetch.
func (s *Server) ticker(ctx context.Context) {
	t := time.NewTicker(eventInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.hub.count() == 0 {
				continue
			}
			s.mu.Lock()
			if s.loaded {
				_ = s.app.RefreshLive(ctx)
				s.lastLive = time.Now()
			}
			s.mu.Unlock()
			s.hub.broadcast(`{"type":"sessions"}`)
		}
	}
}

// checkLoopback refuses any listen host that is not loopback.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("serve: bad address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("serve: refusing to bind %q; only 127.0.0.1 or localhost is allowed", host)
	}
	return nil
}

// loadOrCreateToken reads RecallDir/serve.token or writes a fresh one with
// mode 0600. Any failure falls back to an in-memory token.
func loadOrCreateToken(recallDir string) string {
	if recallDir == "" {
		return ""
	}
	path := filepath.Join(recallDir, TokenFile)
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); tokenRe.MatchString(t) {
			return t
		}
	}
	t := randomToken()
	if err := os.MkdirAll(recallDir, 0o700); err != nil {
		return t
	}
	_ = os.WriteFile(path, []byte(t+"\n"), 0o600)
	return t
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// rand.Read only fails when the OS entropy source is broken; a
		// time-based fallback is still unguessable enough for loopback.
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (s *Server) routes() {
	m := http.NewServeMux()
	m.HandleFunc("/", s.handleIndex)
	m.HandleFunc("/api/sessions", s.api(s.handleSessions, http.MethodGet))
	m.HandleFunc("/api/refresh", s.api(s.handleRefresh, http.MethodPost))
	m.HandleFunc("/api/open", s.api(s.handleOpen, http.MethodPost))
	m.HandleFunc("/api/label", s.api(s.handleLabel, http.MethodPost))
	m.HandleFunc("/api/pin", s.api(s.handlePin, http.MethodPost))
	m.HandleFunc("/api/hide", s.api(s.handleHide, http.MethodPost))
	m.HandleFunc("/api/events", s.api(s.handleEvents, http.MethodGet))
	s.mux = m
}

// csp is the content security policy for the page. Scripts are inline
// only, styles may come from Google Fonts, and nothing else loads.
const csp = "default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src https://fonts.gstatic.com; script-src 'unsafe-inline'; connect-src 'self'; " +
	"img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

func securityHeaders(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", csp)
}

// tokenOK compares t with the server token in constant time.
func (s *Server) tokenOK(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

// sameOrigin refuses requests that a browser marks as coming from another
// site. Requests without any fetch metadata (curl) pass; the token guards
// them.
func sameOrigin(r *http.Request) bool {
	switch strings.ToLower(r.Header.Get("Sec-Fetch-Site")) {
	case "cross-site", "same-site":
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if origin == "null" {
		return false
	}
	return strings.EqualFold(origin, "http://"+r.Host)
}

// api wraps an API handler with method, origin and token checks.
func (s *Server) api(h http.HandlerFunc, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w.Header())
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		t := r.Header.Get("X-Recall-Token")
		if t == "" && r.URL.Path == "/api/events" {
			// EventSource cannot set headers; the stream accepts ?t=.
			t = r.URL.Query().Get("t")
		}
		if !s.tokenOK(t) {
			writeError(w, http.StatusUnauthorized, "missing or wrong token")
			return
		}
		h(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w.Header())
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.tokenOK(r.URL.Query().Get("t")) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>recall</title>"+
			"<p style=\"font: 17px/1.6 system-ui; padding: 40px\">recall serve: missing or wrong token. "+
			"Open the exact URL that <code>recall serve</code> printed.</p>")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.page)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

// decode reads a small JSON body into v.
func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	return nil
}
