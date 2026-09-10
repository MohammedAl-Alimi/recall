package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// Fixture session ids. Nothing here comes from a real machine.
const (
	sidTitled   = "11111111-1111-4111-8111-111111111111"
	sidPlain    = "22222222-2222-4222-8222-222222222222"
	sidHeadless = "33333333-3333-4333-8333-333333333333"
	sidGhost    = "44444444-4444-4444-8444-444444444444"

	testToken = "0123456789abcdef0123456789abcdef"

	// visibleTotal is how many fixture sessions GET /api/sessions counts
	// with the default toggles: the ghost is not in the list, the headless
	// one is filtered out, so only the two closed transcripts remain.
	visibleTotal = 2
)

// testServer bundles a Server with the temp dirs its App was built on.
type testServer struct {
	*Server
	claudeDir string
	recallDir string
	// projCwd is the working directory the fixture transcripts recorded.
	projCwd string
}

// newTestServer builds a Server over a throwaway Claude directory holding
// three hand-written transcripts plus a history.jsonl ghost. Every path is
// a t.TempDir, so no test can read or write a real ~/.claude or ~/.recall.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	claudeDir, recallDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("RECALL_DIR", recallDir)
	t.Setenv("RECALL_DRY_RUN", "1")
	// Keep terminal detection deterministic: no cmux workspace, and a
	// Terminal.app style host so launch never picks the iTerm script.
	t.Setenv("CMUX_WORKSPACE_ID", "")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")

	projCwd := writeFixture(t, claudeDir)
	a, err := app.New(model.PathsFrom(claudeDir, recallDir))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	a.ShellCwd = projCwd
	s := New(a, Options{Token: testToken})
	return &testServer{Server: s, claudeDir: claudeDir, recallDir: recallDir, projCwd: projCwd}
}

// encodeProject mirrors how Claude names a project directory: every rune
// that is not alphanumeric becomes a dash.
func encodeProject(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
}

// writeFixture synthesizes a Claude config tree with three transcripts (a
// closed session with an ai-title, a plain closed session and a headless
// sdk-cli run) and a history.jsonl line for a session with no transcript,
// which the scanner reports as a ghost. It returns the recorded cwd.
func writeFixture(t *testing.T, claudeDir string) string {
	t.Helper()
	cwd := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(claudeDir, "projects", encodeProject(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	rec := func(sid, typ, uuid, parent, ts, entrypoint, content string) string {
		m := map[string]any{
			"parentUuid": nil, "isSidechain": false, "type": typ, "uuid": uuid,
			"timestamp": ts, "cwd": cwd, "sessionId": sid, "version": "2.1.230",
			"gitBranch": "main", "entrypoint": entrypoint, "promptId": "p-" + uuid,
		}
		if parent != "" {
			m["parentUuid"] = parent
		}
		if typ == "user" {
			m["message"] = map[string]any{"role": "user", "content": content}
		} else {
			m["message"] = map[string]any{
				"role": "assistant", "model": "claude-test",
				"content": []map[string]any{{"type": "text", "text": content}},
				"usage": map[string]any{
					"input_tokens": 100, "cache_read_input_tokens": 50,
					"cache_creation_input_tokens": 0, "output_tokens": 20,
				},
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write := func(sid string, lines []string) {
		p := filepath.Join(dir, sid+".jsonl")
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(sidTitled, []string{
		rec(sidTitled, "user", "u1", "", "2026-09-01T10:00:00Z", "cli", "Add a login page to the app"),
		rec(sidTitled, "assistant", "a1", "u1", "2026-09-01T10:00:05Z", "cli", "Done, the login page is in place."),
		rec(sidTitled, "user", "u1b", "a1", "2026-09-01T10:01:00Z", "cli", "Now add a logout button"),
		rec(sidTitled, "assistant", "a1b", "u1b", "2026-09-01T10:01:04Z", "cli", "The logout button is wired up."),
		`{"type":"ai-title","aiTitle":"Login page","sessionId":"` + sidTitled + `"}`,
	})
	write(sidPlain, []string{
		rec(sidPlain, "user", "u2", "", "2026-09-02T10:00:00Z", "cli", "Fix the flaky test in the scanner"),
		rec(sidPlain, "assistant", "a2", "u2", "2026-09-02T10:00:05Z", "cli", "The test is fixed."),
		rec(sidPlain, "user", "u2b", "a2", "2026-09-02T10:02:00Z", "cli", "Run it once more"),
		rec(sidPlain, "assistant", "a2b", "u2b", "2026-09-02T10:02:06Z", "cli", "It passes."),
	})
	write(sidHeadless, []string{
		rec(sidHeadless, "user", "u3", "", "2026-09-03T10:00:00Z", "sdk-cli", "List the exported functions"),
		rec(sidHeadless, "assistant", "a3", "u3", "2026-09-03T10:00:03Z", "sdk-cli", "Here they are."),
	})

	hist := fmt.Sprintf(
		`{"display":"what is flock","pastedContents":{},"timestamp":1756720800000,"project":%q,"sessionId":%q}`+"\n",
		cwd, sidGhost)
	if err := os.WriteFile(filepath.Join(claudeDir, "history.jsonl"), []byte(hist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"cleanupPeriodDays": 365}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return cwd
}

// req builds a request against the in-process handler.
func req(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

// do runs one request through the handler and returns the recorder.
func (ts *testServer) do(t *testing.T, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	ts.Handler().ServeHTTP(w, r)
	return w
}

// api runs an authenticated API request.
func (ts *testServer) api(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := req(t, method, target, body)
	r.Header.Set("X-Recall-Token", testToken)
	return ts.do(t, r)
}

// sessions fetches GET /api/sessions and decodes the payload.
func (ts *testServer) sessions(t *testing.T, query string) Payload {
	t.Helper()
	w := ts.api(t, http.MethodGet, "/api/sessions"+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions%s = %d, body %s", query, w.Code, w.Body.String())
	}
	var p Payload
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode payload: %v (body %s)", err, w.Body.String())
	}
	return p
}

func rowByID(p Payload, id string) *Row {
	for i := range p.Sessions {
		if p.Sessions[i].ID == id {
			return &p.Sessions[i]
		}
	}
	return nil
}

// decodeMap decodes a JSON object response.
func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode json: %v (body %s)", err, w.Body.String())
	}
	return m
}

// readMeta reads meta.json straight from the temp recall directory.
func readMeta(t *testing.T, recallDir string) *state.Meta {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(recallDir, state.MetaFile))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	m := state.NewMeta()
	if err := json.Unmarshal(b, m); err != nil {
		t.Fatalf("decode meta.json: %v (body %s)", err, b)
	}
	return m
}

func TestFixtureLoadsExpectedSessions(t *testing.T) {
	ts := newTestServer(t)
	p := ts.sessions(t, "?hidden=1&ghosts=1&headless=1")
	for _, id := range []string{sidTitled, sidPlain, sidHeadless, sidGhost} {
		if rowByID(p, id) == nil {
			t.Errorf("session %s missing from the payload", id)
		}
	}
	if r := rowByID(p, sidTitled); r != nil && r.Title != "Login page" {
		t.Errorf("ai-title not applied: title = %q", r.Title)
	}
	if r := rowByID(p, sidHeadless); r != nil && !r.Headless {
		t.Error("sdk-cli transcript should be headless")
	}
	if r := rowByID(p, sidGhost); r != nil && !r.Ghost {
		t.Error("history-only session should be a ghost")
	}
}

func TestSessionsRequiresToken(t *testing.T) {
	ts := newTestServer(t)

	if w := ts.do(t, req(t, http.MethodGet, "/api/sessions", "")); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", w.Code)
	}
	r := req(t, http.MethodGet, "/api/sessions", "")
	r.Header.Set("X-Recall-Token", "ffffffffffffffffffffffffffffffff")
	if w := ts.do(t, r); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", w.Code)
	}

	p := ts.sessions(t, "")
	if p.Total != visibleTotal {
		t.Errorf("Total = %d, want %d", p.Total, visibleTotal)
	}
	if len(p.Sessions) != visibleTotal {
		t.Errorf("len(Sessions) = %d, want %d", len(p.Sessions), visibleTotal)
	}
	if p.Summary != "2 sessions" {
		t.Errorf("Summary = %q", p.Summary)
	}
	if !p.DryRun {
		t.Error("DryRun should be true with RECALL_DRY_RUN=1")
	}
}

func TestSessionsTokenInQueryIsNotAccepted(t *testing.T) {
	ts := newTestServer(t)
	// Only /api/events falls back to ?t=; every other endpoint wants the
	// header, so a query token must not be enough.
	w := ts.do(t, req(t, http.MethodGet, "/api/sessions?t="+testToken, ""))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestIndexPage(t *testing.T) {
	ts := newTestServer(t)

	w := ts.do(t, req(t, http.MethodGet, "/?t="+testToken, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != csp {
		t.Errorf("Content-Security-Policy = %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if !strings.Contains(w.Body.String(), "<!doctype html") && !strings.Contains(w.Body.String(), "<!DOCTYPE html") {
		t.Errorf("body does not look like the dashboard page: %.80q", w.Body.String())
	}

	// Without a token the page is a 401 hint page, still text/html and
	// still carrying the security headers.
	w = ts.do(t, req(t, http.MethodGet, "/", ""))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("no token: Content-Type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "missing or wrong token") {
		t.Errorf("no token: body = %q", w.Body.String())
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("no token: X-Frame-Options = %q", got)
	}
}

func TestIndexUnknownPathIs404(t *testing.T) {
	ts := newTestServer(t)
	w := ts.do(t, req(t, http.MethodGet, "/nope?t="+testToken, ""))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestMethodEnforcement(t *testing.T) {
	ts := newTestServer(t)
	cases := []struct {
		method, path, allow string
	}{
		{http.MethodGet, "/api/open", http.MethodPost},
		{http.MethodPost, "/api/sessions", http.MethodGet},
		{http.MethodGet, "/api/label", http.MethodPost},
		{http.MethodGet, "/api/pin", http.MethodPost},
		{http.MethodGet, "/api/hide", http.MethodPost},
		{http.MethodGet, "/api/refresh", http.MethodPost},
		{http.MethodPost, "/api/events", http.MethodGet},
	}
	for _, c := range cases {
		t.Run(c.method+c.path, func(t *testing.T) {
			w := ts.api(t, c.method, c.path, "")
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405 (body %s)", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Allow"); got != c.allow {
				t.Errorf("Allow = %q, want %q", got, c.allow)
			}
		})
	}

	// The method check runs before the token check, so a wrong method is
	// 405 even without any credentials.
	w := ts.do(t, req(t, http.MethodGet, "/api/open", ""))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("unauthenticated wrong method: status = %d, want 405", w.Code)
	}

	// The index page only answers GET and HEAD.
	w = ts.do(t, req(t, http.MethodPut, "/?t="+testToken, ""))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT /: status = %d, want 405", w.Code)
	}
}

func TestCrossOriginRefused(t *testing.T) {
	ts := newTestServer(t)
	body := `{"id":"` + sidTitled + `"}`

	r := req(t, http.MethodPost, "/api/open", body)
	r.Header.Set("X-Recall-Token", testToken)
	r.Header.Set("Origin", "http://evil.example")
	if w := ts.do(t, r); w.Code != http.StatusForbidden {
		t.Errorf("evil Origin: status = %d, want 403", w.Code)
	}

	for _, site := range []string{"cross-site", "same-site"} {
		r := req(t, http.MethodPost, "/api/open", body)
		r.Header.Set("X-Recall-Token", testToken)
		r.Header.Set("Sec-Fetch-Site", site)
		if w := ts.do(t, r); w.Code != http.StatusForbidden {
			t.Errorf("Sec-Fetch-Site %s: status = %d, want 403", site, w.Code)
		}
	}

	r = req(t, http.MethodPost, "/api/open", body)
	r.Header.Set("X-Recall-Token", testToken)
	r.Header.Set("Origin", "null")
	if w := ts.do(t, r); w.Code != http.StatusForbidden {
		t.Errorf("Origin null: status = %d, want 403", w.Code)
	}

	// A same-origin fetch is accepted and reaches the handler.
	r = req(t, http.MethodPost, "/api/open", body)
	r.Header.Set("X-Recall-Token", testToken)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if w := ts.do(t, r); w.Code != http.StatusOK {
		t.Errorf("Sec-Fetch-Site same-origin: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	// So is a matching Origin header.
	r = req(t, http.MethodPost, "/api/open", body)
	r.Header.Set("X-Recall-Token", testToken)
	r.Header.Set("Origin", "http://"+r.Host)
	if w := ts.do(t, r); w.Code != http.StatusOK {
		t.Errorf("matching Origin: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	// The origin check runs before the token check.
	r = req(t, http.MethodPost, "/api/open", body)
	r.Header.Set("Origin", "http://evil.example")
	if w := ts.do(t, r); w.Code != http.StatusForbidden {
		t.Errorf("evil Origin without token: status = %d, want 403", w.Code)
	}
}

func TestOpenDryRun(t *testing.T) {
	ts := newTestServer(t)
	w := ts.api(t, http.MethodPost, "/api/open", `{"id":"`+sidTitled+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var res OpenResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if !res.OK {
		t.Errorf("OK = false, body %s", w.Body.String())
	}
	if !res.DryRun {
		t.Error("DryRun = false, want true with RECALL_DRY_RUN=1")
	}
	if res.Kind != launch.KindResume {
		t.Errorf("Kind = %q, want %q", res.Kind, launch.KindResume)
	}
	want := "claude --resume " + sidTitled
	if !strings.Contains(res.Command, want) {
		t.Errorf("Command = %q, want it to contain %q", res.Command, want)
	}
	if !strings.HasPrefix(res.Command, "cd ") {
		t.Errorf("Command = %q, want a cd prefix", res.Command)
	}
}

func TestOpenGhostAndUnknown(t *testing.T) {
	ts := newTestServer(t)

	w := ts.api(t, http.MethodPost, "/api/open", `{"id":"`+sidGhost+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("ghost: status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	if msg, _ := decodeMap(t, w)["error"].(string); !strings.Contains(msg, "no transcript left to resume") {
		t.Errorf("ghost: error = %q", msg)
	}

	w = ts.api(t, http.MethodPost, "/api/open", `{"id":"99999999-9999-4999-8999-999999999999"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	if msg, _ := decodeMap(t, w)["error"].(string); !strings.Contains(msg, "no session matches") {
		t.Errorf("unknown id: error = %q", msg)
	}

	// An empty id is a lookup failure too, not a decode failure.
	w = ts.api(t, http.MethodPost, "/api/open", `{"id":""}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("empty id: status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}

	// An unknown target is rejected before anything is looked up.
	w = ts.api(t, http.MethodPost, "/api/open", `{"id":"`+sidTitled+`","target":"emacs"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad target: status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}

	// Unknown JSON fields are refused by the decoder.
	w = ts.api(t, http.MethodPost, "/api/open", `{"id":"`+sidTitled+`","nope":1}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}

func TestOpenTargetCmux(t *testing.T) {
	ts := newTestServer(t)
	w := ts.api(t, http.MethodPost, "/api/open", `{"id":"`+sidTitled+`","target":"cmux"}`)

	if _, ok := launch.CmuxAvailable(); !ok {
		// No cmux CLI on this machine: the target is refused up front.
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if msg, _ := decodeMap(t, w)["error"].(string); !strings.Contains(msg, "cmux is not installed") {
			t.Errorf("error = %q", msg)
		}
		return
	}

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var res OpenResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Target != launch.TerminalCmux {
		t.Errorf("Target = %q", res.Target)
	}
	if !res.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !strings.Contains(res.Command, "new-workspace") {
		t.Errorf("Command = %q, want it to contain %q", res.Command, "new-workspace")
	}
	if !strings.Contains(res.Command, "claude --resume "+sidTitled) {
		t.Errorf("Command = %q, want the resume command inside the workspace command", res.Command)
	}
}

func TestLabelPersists(t *testing.T) {
	ts := newTestServer(t)
	w := ts.api(t, http.MethodPost, "/api/label", `{"id":"`+sidTitled+`","label":"scanner-work"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if ok, _ := decodeMap(t, w)["ok"].(bool); !ok {
		t.Errorf("ok = false, body %s", w.Body.String())
	}
	if got := readMeta(t, ts.recallDir).Labels[sidTitled]; got != "scanner-work" {
		t.Errorf("meta.json label = %q, want %q", got, "scanner-work")
	}
	if r := rowByID(ts.sessions(t, ""), sidTitled); r == nil || r.Label != "scanner-work" {
		t.Errorf("GET /api/sessions does not show the label: %+v", r)
	}

	// Newlines are folded into spaces before the length check.
	w = ts.api(t, http.MethodPost, "/api/label", `{"id":"`+sidTitled+`","label":"two\nlines"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("multiline label: status = %d, body %s", w.Code, w.Body.String())
	}
	if got := readMeta(t, ts.recallDir).Labels[sidTitled]; got != "two lines" {
		t.Errorf("meta.json label = %q, want %q", got, "two lines")
	}

	// Too long is a 400 and leaves the stored label alone.
	long := strings.Repeat("x", maxLabelLen+1)
	w = ts.api(t, http.MethodPost, "/api/label", `{"id":"`+sidTitled+`","label":"`+long+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("long label: status = %d, want 400", w.Code)
	}
	if got := readMeta(t, ts.recallDir).Labels[sidTitled]; got != "two lines" {
		t.Errorf("long label changed meta.json to %q", got)
	}

	// An unknown session is a 404.
	w = ts.api(t, http.MethodPost, "/api/label", `{"id":"99999999-9999-4999-8999-999999999999","label":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404", w.Code)
	}
}

func TestPinPersists(t *testing.T) {
	ts := newTestServer(t)
	w := ts.api(t, http.MethodPost, "/api/pin", `{"id":"`+sidPlain+`","pinned":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	m := readMeta(t, ts.recallDir)
	if !contains(m.Pinned, sidPlain) {
		t.Errorf("meta.json Pinned = %v, want it to contain %s", m.Pinned, sidPlain)
	}
	if r := rowByID(ts.sessions(t, ""), sidPlain); r == nil || !r.Pinned {
		t.Errorf("GET /api/sessions does not show the pin: %+v", r)
	}

	if w := ts.api(t, http.MethodPost, "/api/pin", `{"id":"`+sidPlain+`","pinned":false}`); w.Code != http.StatusOK {
		t.Fatalf("unpin: status = %d, body %s", w.Code, w.Body.String())
	}
	if m := readMeta(t, ts.recallDir); contains(m.Pinned, sidPlain) {
		t.Errorf("meta.json Pinned = %v after unpin", m.Pinned)
	}
	if r := rowByID(ts.sessions(t, ""), sidPlain); r == nil || r.Pinned {
		t.Errorf("GET /api/sessions still shows the pin: %+v", r)
	}
}

func TestHidePersists(t *testing.T) {
	ts := newTestServer(t)
	w := ts.api(t, http.MethodPost, "/api/hide", `{"id":"`+sidPlain+`","hidden":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if m := readMeta(t, ts.recallDir); !contains(m.Hidden, sidPlain) {
		t.Errorf("meta.json Hidden = %v, want it to contain %s", m.Hidden, sidPlain)
	}

	p := ts.sessions(t, "")
	if rowByID(p, sidPlain) != nil {
		t.Error("hidden session still listed by default")
	}
	if p.Total != visibleTotal-1 {
		t.Errorf("Total = %d, want %d", p.Total, visibleTotal-1)
	}
	if r := rowByID(ts.sessions(t, "?hidden=1"), sidPlain); r == nil || !r.Hidden {
		t.Errorf("?hidden=1 does not show the hidden session: %+v", r)
	}

	if w := ts.api(t, http.MethodPost, "/api/hide", `{"id":"`+sidPlain+`","hidden":false}`); w.Code != http.StatusOK {
		t.Fatalf("unhide: status = %d, body %s", w.Code, w.Body.String())
	}
	if m := readMeta(t, ts.recallDir); contains(m.Hidden, sidPlain) {
		t.Errorf("meta.json Hidden = %v after unhide", m.Hidden)
	}
	if rowByID(ts.sessions(t, ""), sidPlain) == nil {
		t.Error("unhidden session missing from the default list")
	}
}

func TestRefreshReloads(t *testing.T) {
	ts := newTestServer(t)
	first := ts.sessions(t, "")
	if first.Total != visibleTotal {
		t.Fatalf("Total = %d, want %d", first.Total, visibleTotal)
	}
	w := ts.api(t, http.MethodPost, "/api/refresh", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var p Payload
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Total != visibleTotal {
		t.Errorf("refresh Total = %d, want %d", p.Total, visibleTotal)
	}
	if !p.LoadedAt.After(first.LoadedAt) && !p.LoadedAt.Equal(first.LoadedAt) {
		t.Errorf("LoadedAt went backwards: %v then %v", first.LoadedAt, p.LoadedAt)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestCheckLoopback(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:4747", true},
		{"127.0.0.1:0", true},
		{"localhost:4747", true},
		{"[::1]:4747", true},
		{"127.0.0.5:4747", true},
		{DefaultAddr, true},
		{"0.0.0.0:4747", false},
		{"192.0.2.10:4747", false},
		{"[2001:db8::1]:4747", false},
		{"example.com:4747", false},
		{"", false},
		{"127.0.0.1", false},
	}
	for _, c := range cases {
		err := checkLoopback(c.addr)
		if c.ok && err != nil {
			t.Errorf("checkLoopback(%q) = %v, want nil", c.addr, err)
		}
		if !c.ok && err == nil {
			t.Errorf("checkLoopback(%q) = nil, want an error", c.addr)
		}
	}
}

func TestListenAndServeRefusesNonLoopback(t *testing.T) {
	ts := newTestServer(t)
	ts.opts.Addr = "0.0.0.0:0"
	err := ts.ListenAndServe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing to bind") {
		t.Fatalf("ListenAndServe = %v, want a refusal", err)
	}
}

func TestListenAndServeWithoutApp(t *testing.T) {
	s := New(nil, Options{Token: testToken, Addr: "127.0.0.1:0"})
	if err := s.ListenAndServe(context.Background()); err == nil || !strings.Contains(err.Error(), "no app") {
		t.Fatalf("ListenAndServe = %v, want a no-app error", err)
	}
}

func TestTokenFileCreatedAndReused(t *testing.T) {
	claudeDir, recallDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", t.TempDir())
	a, err := app.New(model.PathsFrom(claudeDir, recallDir))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	s1 := New(a, Options{})
	tok := s1.Token()
	if !tokenRe.MatchString(tok) {
		t.Fatalf("generated token %q does not match %s", tok, tokenRe)
	}

	path := filepath.Join(recallDir, TokenFile)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", TokenFile, err)
	}
	if perm := st.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("mode = %v, want 0600", perm)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != tok {
		t.Errorf("file holds %q, Token() is %q", strings.TrimSpace(string(b)), tok)
	}

	if s2 := New(a, Options{}); s2.Token() != tok {
		t.Errorf("second New minted a new token: %q then %q", tok, s2.Token())
	}

	// An explicit token wins over the stored one and is not written back.
	if s3 := New(a, Options{Token: testToken}); s3.Token() != testToken {
		t.Errorf("explicit token ignored: %q", s3.Token())
	}
	if b2, _ := os.ReadFile(path); strings.TrimSpace(string(b2)) != tok {
		t.Errorf("explicit token overwrote the file: %q", strings.TrimSpace(string(b2)))
	}
}

func TestTokenFileGarbageIsReplaced(t *testing.T) {
	claudeDir, recallDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(recallDir, TokenFile)
	if err := os.WriteFile(path, []byte("not-a-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(model.PathsFrom(claudeDir, recallDir))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	s := New(a, Options{})
	if !tokenRe.MatchString(s.Token()) {
		t.Fatalf("token %q does not match %s", s.Token(), tokenRe)
	}
	b, _ := os.ReadFile(path)
	if strings.TrimSpace(string(b)) != s.Token() {
		t.Errorf("garbage token not replaced on disk: %q", strings.TrimSpace(string(b)))
	}
}

func TestURLAndAddrDefaults(t *testing.T) {
	ts := newTestServer(t)
	if ts.Addr() != DefaultAddr {
		t.Errorf("Addr() = %q, want %q", ts.Addr(), DefaultAddr)
	}
	if want := "http://" + DefaultAddr + "/?t=" + testToken; ts.URL() != want {
		t.Errorf("URL() = %q, want %q", ts.URL(), want)
	}
}

func TestEventsStream(t *testing.T) {
	ts := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := req(t, http.MethodGet, "/api/events?t="+testToken, "").WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ts.Handler().ServeHTTP(w, r)
	}()

	// The hello frame is written and flushed before the handler blocks, so
	// it is on the recorder as soon as one client is registered.
	deadline := time.Now().Add(2 * time.Second)
	for ts.hub.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ts.hub.count() == 0 {
		cancel()
		<-done
		t.Fatal("no SSE client registered within the timeout")
	}
	ts.hub.broadcast(`{"type":"sessions"}`)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleEvents did not return after the request context was cancelled")
	}

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data: {"type":"hello"}`) {
		t.Errorf("no hello frame in the stream: %q", body)
	}
	if !strings.Contains(body, "retry: 3000") {
		t.Errorf("no retry hint in the stream: %q", body)
	}
	if ts.hub.count() != 0 {
		t.Errorf("client not removed from the hub: count = %d", ts.hub.count())
	}
}

func TestEventsRequiresToken(t *testing.T) {
	ts := newTestServer(t)
	if w := ts.do(t, req(t, http.MethodGet, "/api/events", "")); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", w.Code)
	}
	if w := ts.do(t, req(t, http.MethodGet, "/api/events?t=bad", "")); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong query token: status = %d, want 401", w.Code)
	}
}

func TestHubBroadcastAndClose(t *testing.T) {
	h := newHub()
	if h.count() != 0 {
		t.Fatalf("count = %d, want 0", h.count())
	}
	a, b := h.add(), h.add()
	if h.count() != 2 {
		t.Fatalf("count = %d, want 2", h.count())
	}
	h.broadcast("x")
	if got := <-a; got != "x" {
		t.Errorf("client a got %q", got)
	}
	if got := <-b; got != "x" {
		t.Errorf("client b got %q", got)
	}

	// A full buffer drops the message instead of blocking.
	for i := 0; i < 100; i++ {
		h.broadcast("y")
	}

	h.remove(a)
	if h.count() != 1 {
		t.Errorf("count after remove = %d, want 1", h.count())
	}
	h.remove(a) // removing twice must not close a closed channel
	h.closeAll()
	if h.count() != 0 {
		t.Errorf("count after closeAll = %d, want 0", h.count())
	}
	if _, open := <-b; open {
		// b still holds buffered messages; drain to the close.
		for range b {
		}
	}
}

func TestSummaryLine(t *testing.T) {
	cases := []struct {
		total, running, needs int
		want                  string
	}{
		{0, 0, 0, "0 sessions"},
		{1, 0, 0, "1 session"},
		{3, 1, 0, "3 sessions · 1 running"},
		{3, 1, 1, "3 sessions · 1 running · 1 needs you"},
		{3, 0, 2, "3 sessions · 2 need you"},
	}
	for _, c := range cases {
		if got := summaryLine(c.total, c.running, c.needs); got != c.want {
			t.Errorf("summaryLine(%d,%d,%d) = %q, want %q", c.total, c.running, c.needs, got, c.want)
		}
	}
}

func TestTerminalFor(t *testing.T) {
	for _, target := range []string{"", "newtab"} {
		if got, err := terminalFor(target); got != "" || err != nil {
			t.Errorf("terminalFor(%q) = %q, %v", target, got, err)
		}
	}
	for _, target := range []string{"terminal", "iterm"} {
		if got, err := terminalFor(target); got != target || err != nil {
			t.Errorf("terminalFor(%q) = %q, %v", target, got, err)
		}
	}
	if _, err := terminalFor("emacs"); err == nil {
		t.Error("terminalFor(\"emacs\") = nil error, want one")
	}
	got, err := terminalFor(launch.TerminalCmux)
	if _, ok := launch.CmuxAvailable(); ok {
		if got != launch.TerminalCmux || err != nil {
			t.Errorf("terminalFor(cmux) = %q, %v", got, err)
		}
	} else if err == nil {
		t.Error("terminalFor(cmux) = nil error on a machine without cmux")
	}
}

func TestCommandLine(t *testing.T) {
	cases := []struct {
		cwd  string
		argv []string
		want string
	}{
		{"", nil, ""},
		{"/Users/me/dev/app", nil, ""},
		{"", []string{"claude", "--resume", "x"}, "claude --resume x"},
		{"/Users/me/dev/app", []string{"claude"}, "cd /Users/me/dev/app && claude"},
		{"/Users/me/my app", []string{"claude"}, "cd '/Users/me/my app' && claude"},
	}
	for _, c := range cases {
		if got := commandLine(c.cwd, c.argv); got != c.want {
			t.Errorf("commandLine(%q, %v) = %q, want %q", c.cwd, c.argv, got, c.want)
		}
	}
}

func TestDryRunHelper(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "1")
	if !dryRun() {
		t.Error("dryRun() = false with RECALL_DRY_RUN=1")
	}
	t.Setenv("RECALL_DRY_RUN", "0")
	if dryRun() {
		t.Error("dryRun() = true with RECALL_DRY_RUN=0")
	}
}

func TestFlagQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/sessions?a=1&b=true&c=0&d=yes", nil)
	for _, name := range []string{"a", "b"} {
		if !flag(r, name) {
			t.Errorf("flag(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"c", "d", "missing"} {
		if flag(r, name) {
			t.Errorf("flag(%q) = true, want false", name)
		}
	}
}
