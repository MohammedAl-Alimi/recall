package scan

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

const (
	sidNormal    = "11111111-1111-4111-8111-111111111111"
	sidCustom    = "22222222-2222-4222-8222-222222222222"
	sidAgent     = "33333333-3333-4333-8333-333333333333"
	sidBigResult = "44444444-4444-4444-8444-444444444444"
	sidCompact   = "55555555-5555-4555-8555-555555555555"
	sidDangling  = "66666666-6666-4666-8666-666666666666"
	sidHeadless  = "77777777-7777-4777-8777-777777777777"
	sidNoEnv     = "88888888-8888-4888-8888-888888888888"
	sidOld       = "99999999-9999-4999-8999-999999999999"
	projectName  = "-Users-alice-dev-app"
)

// fixtureDir is the committed, hand-synthesized Claude config tree.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixtures missing: %v", err)
	}
	return dir
}

// tempPaths copies the fixtures into a temp dir so tests never touch a real
// ~/.claude and can mutate transcripts freely.
func tempPaths(t *testing.T) model.Paths {
	t.Helper()
	root := t.TempDir()
	claude := filepath.Join(root, "claude")
	copyTree(t, fixtureDir(t), claude)
	return model.PathsFrom(claude, filepath.Join(root, "recall"))
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func transcriptPath(p model.Paths, sid string) string {
	return filepath.Join(p.ProjectsDir, projectName, sid+".jsonl")
}

func byID(list []*model.Session) map[string]*model.Session {
	m := map[string]*model.Session{}
	for _, s := range list {
		m[s.ID] = s
	}
	return m
}

func mustScan(t *testing.T, s *Scanner) []*model.Session {
	t.Helper()
	list, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return list
}

func TestEncodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"/Users/me/dev/recall":    "-Users-me-dev-recall",
		"/tmp/a b.c":              "-tmp-a-b-c",
		"C:\\work":                "C--work",
		"":                        "",
		"/Users/me/Work @act.3 /": "-Users-me-Work--act-3--",
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTranscriptName(t *testing.T) {
	cases := map[string]bool{
		sidNormal + ".jsonl":                          true,
		sidNormal + ".jsonl.orphaned-1700000000000":   false,
		sidNormal + ".superseded-1700000000000.jsonl": false,
		"notes.txt":                     false,
		".hidden.jsonl":                 false,
		"agent-a1b2.jsonl":              true,
		".jsonl":                        false,
		sidNormal + ".orphaned-1.jsonl": false,
	}
	for name, want := range cases {
		if got := isTranscriptName(name); got != want {
			t.Errorf("isTranscriptName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestScanDiscoversOnlyTopLevelTranscripts(t *testing.T) {
	p := tempPaths(t)
	s := New(p)
	list := mustScan(t, s)
	if len(list) != 9 {
		for _, x := range list {
			t.Logf("  %s %s", x.ID, x.Path)
		}
		t.Fatalf("got %d sessions, want 9", len(list))
	}
	m := byID(list)
	for _, sid := range []string{sidNormal, sidCustom, sidAgent, sidBigResult, sidCompact, sidDangling, sidHeadless, sidNoEnv, sidOld} {
		sess, ok := m[sid]
		if !ok {
			t.Errorf("missing session %s", sid)
			continue
		}
		if sess.ProjectDir != filepath.Join(p.ProjectsDir, projectName) {
			t.Errorf("%s ProjectDir = %q", sid, sess.ProjectDir)
		}
		if sess.Path != transcriptPath(p, sid) {
			t.Errorf("%s Path = %q", sid, sess.Path)
		}
		if sess.Ghost {
			t.Errorf("%s flagged as ghost", sid)
		}
		if sess.Offset <= 0 || sess.Offset != sess.Size {
			t.Errorf("%s Offset = %d Size = %d, want equal and > 0", sid, sess.Offset, sess.Size)
		}
		if sess.Inode == 0 {
			t.Errorf("%s Inode = 0", sid)
		}
	}
	for _, bad := range []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "agent-a1b2c3d4e5f60718"} {
		if _, ok := m[bad]; ok {
			t.Errorf("session %s should have been skipped", bad)
		}
	}
	// Sorted by LastActive desc.
	for i := 1; i < len(list); i++ {
		if list[i].LastActive.After(list[i-1].LastActive) {
			t.Errorf("not sorted by LastActive desc at %d", i)
		}
	}
	st := s.Stats()
	if st.Full != 9 || st.Cached != 0 || st.Incremental != 0 || st.Failed != 0 {
		t.Errorf("stats after first scan = %+v", st)
	}
	if err := s.CacheError(); err != nil {
		t.Errorf("cache error: %v", err)
	}
	fi, err := os.Stat(filepath.Join(p.RecallDir, "cache.json"))
	if err != nil {
		t.Fatalf("cache.json missing: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache.json perm = %o, want 600", perm)
	}
	dfi, _ := os.Stat(p.RecallDir)
	if perm := dfi.Mode().Perm(); perm != 0o700 {
		t.Errorf("recall dir perm = %o, want 700", perm)
	}
}

func TestScanMissingProjectsDir(t *testing.T) {
	root := t.TempDir()
	p := model.PathsFrom(filepath.Join(root, "nope"), filepath.Join(root, "recall"))
	list, err := New(p).Scan(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("Scan on missing dir = %d, %v; want 0, nil", len(list), err)
	}
}

func TestScanUsesCacheAcrossScannerInstances(t *testing.T) {
	p := tempPaths(t)
	first := mustScan(t, New(p))
	s2 := New(p)
	second := mustScan(t, s2)
	st := s2.Stats()
	if st.Cached != 9 || st.Full != 0 {
		t.Fatalf("second scanner stats = %+v, want all cached", st)
	}
	a, b := byID(first), byID(second)
	for sid, x := range a {
		y := b[sid]
		if y == nil {
			t.Fatalf("%s missing after cached scan", sid)
		}
		if x.Title != y.Title || x.Turns != y.Turns || x.LastPrompt != y.LastPrompt || x.DanglingTool != y.DanglingTool ||
			!x.LastActive.Equal(y.LastActive) || x.Lines != y.Lines || x.WorkCwd != y.WorkCwd || len(x.PRs) != len(y.PRs) {
			t.Errorf("%s differs after cache round trip:\n%+v\n%+v", sid, x, y)
		}
	}
	// Returned sessions are copies: mutating one must not leak into the cache.
	b[sidNormal].Title = "mutated"
	third := byID(mustScan(t, s2))
	if third[sidNormal].Title == "mutated" {
		t.Error("cached session leaked a caller mutation")
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestScanIncrementalAppend(t *testing.T) {
	p := tempPaths(t)
	s := New(p)
	before := byID(mustScan(t, s))[sidHeadless]
	if before.Turns != 1 || !before.Headless || before.TitleSource != "first-prompt" {
		t.Fatalf("unexpected baseline: turns=%d headless=%v source=%s", before.Turns, before.Headless, before.TitleSource)
	}
	path := transcriptPath(p, sidHeadless)

	// Make sure mtime moves even on coarse filesystems.
	time.Sleep(20 * time.Millisecond)
	appendFile(t, path, strings.Join([]string{
		`{"parentUuid":"a-2","isSidechain":false,"userType":"external","cwd":"/Users/alice/dev/app","sessionId":"` + sidHeadless + `","version":"2.1.227","gitBranch":"main","type":"user","message":{"role":"user","content":"Now also list the unexported ones"},"uuid":"u-3","timestamp":"2026-02-11T07:10:00.000Z","entrypoint":"cli","promptId":"p-2"}`,
		`{"parentUuid":"u-3","isSidechain":false,"userType":"external","cwd":"/Users/alice/dev/app","sessionId":"` + sidHeadless + `","version":"2.1.227","gitBranch":"main","type":"assistant","uuid":"a-3","timestamp":"2026-02-11T07:10:03.000Z","message":{"id":"msg-3","type":"message","role":"assistant","model":"claude-synth-1","content":[{"type":"tool_use","id":"toolu_09","name":"Grep","input":{"pattern":"^func [a-z]"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"cache_creation_input_tokens":10,"cache_read_input_tokens":130,"output_tokens":10}},"requestId":"req-3","entrypoint":"cli"}`,
		`{"type":"custom-title","customTitle":"Util exports","sessionId":"` + sidHeadless + `"}`,
	}, "\n")+"\n")

	after := byID(mustScan(t, s))[sidHeadless]
	st := s.Stats()
	if st.Incremental != 1 || st.Cached != 8 || st.Full != 0 {
		t.Fatalf("stats after append = %+v, want 1 incremental + 8 cached", st)
	}
	if after.Turns != 2 {
		t.Errorf("Turns = %d, want 2", after.Turns)
	}
	if after.Title != "Util exports" || after.TitleSource != "custom-title" {
		t.Errorf("Title = %q (%s), want custom title", after.Title, after.TitleSource)
	}
	if after.LastPrompt != "Now also list the unexported ones" {
		t.Errorf("LastPrompt = %q", after.LastPrompt)
	}
	if after.DanglingTool != "Grep" {
		t.Errorf("DanglingTool = %q, want Grep (tail tool_use without result)", after.DanglingTool)
	}
	if after.Headless {
		t.Error("Headless should clear once the session has two prompts and a custom title")
	}
	if want := time.Date(2026, 2, 11, 7, 10, 3, 0, time.UTC); !after.LastActive.Equal(want) {
		t.Errorf("LastActive = %v, want %v", after.LastActive, want)
	}
	if after.Lines != before.Lines+3 {
		t.Errorf("Lines = %d, want %d", after.Lines, before.Lines+3)
	}

	// A partial trailing line is not consumed until its newline arrives.
	time.Sleep(20 * time.Millisecond)
	partial := `{"parentUuid":"a-3","isSidechain":false,"userType":"external","cwd":"/Users/alice/dev/app","sessionId":"` + sidHeadless + `","version":"2.1.227","gitBranch":"main","type":"user","message":{"role":"user","content":"third prompt"},"uuid":"u-4","timestamp":"2026-02-11T07:20:00.000Z","entrypoint":"cli","promptId":"p-3"}`
	appendFile(t, path, partial[:len(partial)/2])
	mid := byID(mustScan(t, s))[sidHeadless]
	if s.Stats().Incremental != 1 {
		t.Fatalf("stats after partial append = %+v", s.Stats())
	}
	if mid.Turns != 2 || mid.ParseErrors != 0 {
		t.Errorf("partial line was consumed: turns=%d errors=%d", mid.Turns, mid.ParseErrors)
	}
	if mid.Offset >= mid.Size {
		t.Errorf("Offset %d should stay before the partial line (size %d)", mid.Offset, mid.Size)
	}
	time.Sleep(20 * time.Millisecond)
	appendFile(t, path, partial[len(partial)/2:]+"\n")
	done := byID(mustScan(t, s))[sidHeadless]
	if s.Stats().Incremental != 1 {
		t.Fatalf("stats after completing line = %+v", s.Stats())
	}
	if done.Turns != 3 || done.LastPrompt != "third prompt" || done.ParseErrors != 0 {
		t.Errorf("completed line not applied: turns=%d last=%q errors=%d", done.Turns, done.LastPrompt, done.ParseErrors)
	}
	if done.Offset != done.Size {
		t.Errorf("Offset %d != Size %d after complete line", done.Offset, done.Size)
	}
	if done.DanglingTool != "" {
		t.Errorf("DanglingTool = %q, want cleared by the new prompt", done.DanglingTool)
	}
}

func TestScanRewrittenFileIsReparsed(t *testing.T) {
	p := tempPaths(t)
	s := New(p)
	mustScan(t, s)
	path := transcriptPath(p, sidCustom)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Drop the custom-title line: the file shrinks, so the cache must not
	// treat it as an append.
	var keep [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"custom-title"`)) {
			keep = append(keep, line)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, bytes.Join(keep, []byte("\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := byID(mustScan(t, s))[sidCustom]
	if st := s.Stats(); st.Full != 1 || st.Cached != 8 {
		t.Fatalf("stats = %+v, want 1 full reparse", st)
	}
	if sess.Title != "planner" || sess.TitleSource != "agent-name" {
		t.Errorf("after rewrite Title = %q (%s), want agent-name planner", sess.Title, sess.TitleSource)
	}
}

func TestScanCancelledContext(t *testing.T) {
	p := tempPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(p).Scan(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestGhosts(t *testing.T) {
	p := tempPaths(t)
	s := New(p)
	known := map[string]bool{sidNormal: true, sidCustom: true}
	ghosts, err := s.Ghosts(known)
	if err != nil {
		t.Fatalf("Ghosts: %v", err)
	}
	if len(ghosts) != 2 {
		for _, g := range ghosts {
			t.Logf("  ghost %s %q", g.ID, g.Title)
		}
		t.Fatalf("got %d ghosts, want 2", len(ghosts))
	}
	g := ghosts[0]
	if g.ID != "c1c1c1c1-c1c1-4c1c-8c1c-c1c1c1c1c1c1" {
		t.Fatalf("first ghost = %s", g.ID)
	}
	if !g.Ghost || g.State != model.StateGhost {
		t.Errorf("ghost flags: Ghost=%v State=%s", g.Ghost, g.State)
	}
	if g.Title != "make it longer please and add a dog" {
		t.Errorf("Title = %q, want longest non-slash prompt", g.Title)
	}
	if g.FirstPrompt != "help me write a haiku about cats" {
		t.Errorf("FirstPrompt = %q", g.FirstPrompt)
	}
	if len(g.GhostPrompts) != 3 || g.GhostPrompts[0] != "/clear" {
		t.Errorf("GhostPrompts = %v", g.GhostPrompts)
	}
	if g.Cwd != "/Users/alice/dev/app" || g.WorkCwd != g.Cwd {
		t.Errorf("Cwd = %q WorkCwd = %q", g.Cwd, g.WorkCwd)
	}
	if want := time.UnixMilli(1770100000000).UTC(); !g.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", g.CreatedAt, want)
	}
	if want := time.UnixMilli(1770100120000).UTC(); !g.LastActive.Equal(want) {
		t.Errorf("LastActive = %v, want %v", g.LastActive, want)
	}
	if g.Turns != 3 {
		t.Errorf("Turns = %d", g.Turns)
	}
	g2 := ghosts[1]
	if g2.ID != "e1e1e1e1-e1e1-4e1e-8e1e-e1e1e1e1e1e1" || g2.Cwd != "/Users/alice/dev/other" || g2.Title != "why does the build fail on linux" {
		t.Errorf("second ghost = %s %q %q", g2.ID, g2.Cwd, g2.Title)
	}
	for _, g := range ghosts {
		switch g.ID {
		case "d1d1d1d1-d1d1-4d1d-8d1d-d1d1d1d1d1d1":
			t.Error("slash-only session must be dropped")
		case sidBigResult:
			t.Error("session with a transcript on disk must not be a ghost even when not in known")
		case sidNormal, sidCustom:
			t.Error("known session reported as ghost")
		}
	}
}

func TestGhostsMissingHistory(t *testing.T) {
	root := t.TempDir()
	p := model.PathsFrom(filepath.Join(root, "claude"), filepath.Join(root, "recall"))
	ghosts, err := New(p).Ghosts(nil)
	if err != nil || len(ghosts) != 0 {
		t.Fatalf("Ghosts without history = %d, %v", len(ghosts), err)
	}
}

func TestRegistryFixtureParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixtureDir(t), "sessions", "48213.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
		ProcStart string `json:"procStart"`
	}
	if err := jsonUnmarshal(data, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.PID != 48213 || reg.SessionID != sidNormal || reg.Status != "idle" || reg.ProcStart == "" {
		t.Errorf("registry fixture = %+v", reg)
	}
}
