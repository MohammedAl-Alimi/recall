package scan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(data, []byte("\n"))
}

func parseFixture(t *testing.T, sid string) *model.Session {
	t.Helper()
	path := filepath.Join(fixtureDir(t), "projects", projectName, sid+".jsonl")
	sess, err := ParseTranscript(path)
	if err != nil {
		t.Fatalf("ParseTranscript(%s): %v", sid, err)
	}
	return sess
}

func TestParseTranscriptTable(t *testing.T) {
	type want struct {
		title, source          string
		cwd, workCwd, lastCwd  string
		branch, entrypoint     string
		first, last, assistant string
		created, lastActive    time.Time
		turns, compactions     int
		permMode               string
		cost                   float64
		prs, artifacts         int
		bridge                 string
		headless               bool
		lineage                string
		interrupted            bool
		dangling               string
		files                  []string
		bgJobs                 int
		versions               []string
		contextTokens          int
		parseErrors            int
		isWorktree             bool
		repoRoot               string
	}
	cases := []struct {
		sid  string
		want want
	}{
		{sidNormal, want{
			title: "Retry helper for http client", source: "ai-title",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "feat/retry", entrypoint: "cli",
			first: "Add a retry helper to the http client", last: "now open a PR for it",
			assistant:  "Opened PR #42 and documented the retry helper.",
			created:    ts("2026-01-15T09:00:00Z"),
			lastActive: ts("2026-01-15T09:05:16Z"),
			turns:      2, compactions: 1, permMode: "default", cost: 0.4321,
			prs: 1, artifacts: 1, bridge: "https://claude.ai/code/session_synth11111111",
			lineage: "root", files: []string{"/Users/alice/dev/app/http/client.go", "/Users/alice/dev/app/docs/retry.md"},
			versions: []string{"2.1.226", "2.1.227"}, contextTokens: 10000, parseErrors: 1,
		}},
		{sidCustom, want{
			title: "Config migration (renamed by hand)", source: "custom-title",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "main", entrypoint: "claude-desktop",
			first: "Plan the migration to the new config format", last: "Plan the migration to the new config format",
			assistant: "Here is a three step plan.",
			created:   ts("2026-02-01T10:00:00Z"), lastActive: ts("2026-02-01T10:00:06Z"),
			turns: 1, permMode: "plan", lineage: "root", versions: []string{"2.1.227"}, contextTokens: 2530,
		}},
		{sidAgent, want{
			title: "cache-fixer", source: "agent-name",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app/.claude/worktrees/fix-cache",
			lastCwd: "/Users/alice/dev/app/.claude/worktrees/fix-cache",
			branch:  "fix-cache", entrypoint: "cli",
			first: "Fix the flaky cache test in a worktree", last: "Fix the flaky cache test in a worktree",
			assistant: "The test now waits for the cache flush.",
			created:   ts("2026-02-03T08:00:00Z"), lastActive: ts("2026-02-03T08:00:09Z"),
			turns: 1, lineage: "root", files: []string{"/Users/alice/dev/app/.claude/worktrees/fix-cache/cache/cache_test.go"},
			versions: []string{"2.1.227"}, contextTokens: 1220, isWorktree: true, repoRoot: "/Users/alice/dev/app",
		}},
		{sidBigResult, want{
			title: "Log error summary", source: "ai-title",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "main", entrypoint: "cli",
			first: "Summarize the errors in that log", last: "Summarize the errors in that log",
			assistant: "No errors: every line reports ok.",
			created:   ts("2026-02-05T12:00:00Z"), lastActive: ts("2026-02-05T12:00:35Z"),
			turns: 1, lineage: "root", versions: []string{"2.1.227"}, contextTokens: 69500,
		}},
		{sidCompact, want{
			title: "Scheduler refactor continued", source: "ai-title",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "main", entrypoint: "cli",
			first: "Continue with the scheduler refactor", last: "Continue with the scheduler refactor",
			assistant: "Continuing: extracting the tick loop next.",
			created:   ts("2026-02-08T14:00:00Z"), lastActive: ts("2026-02-08T14:00:15Z"),
			turns: 1, compactions: 1, lineage: "continuation", versions: []string{"2.1.227"}, contextTokens: 7020,
		}},
		{sidDangling, want{
			title: "Run the full test suite and fix what breaks", source: "first-prompt",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "main", entrypoint: "cli",
			first: "Run the full test suite and fix what breaks", last: "Run the full test suite and fix what breaks",
			assistant: "Watcher started. Running the suite now.",
			created:   ts("2026-02-10T16:00:00Z"), lastActive: ts("2026-02-10T16:00:07Z"),
			turns: 1, lineage: "root", dangling: "Bash", bgJobs: 1, versions: []string{"2.1.227"}, contextTokens: 150,
		}},
		{sidHeadless, want{
			title: "List the exported functions in pkg/util", source: "first-prompt",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "main", entrypoint: "cli",
			first: "List the exported functions in pkg/util", last: "List the exported functions in pkg/util",
			assistant: "One exported function: Title.",
			created:   ts("2026-02-11T07:00:00Z"), lastActive: ts("2026-02-11T07:00:05Z"),
			turns: 1, lineage: "root", headless: true, versions: []string{"2.1.227"}, contextTokens: 140,
		}},
		{sidOld, want{
			title: "Legacy summary title from older CLI", source: "summary",
			cwd: "/Users/alice/dev/app", workCwd: "/Users/alice/dev/app", lastCwd: "/Users/alice/dev/app",
			branch: "main",
			first:  "Explain how the cache eviction works", last: "Explain how the cache eviction works",
			assistant: "Eviction is LRU with a size cap.",
			created:   ts("2025-12-20T18:30:00Z"), lastActive: ts("2025-12-20T18:31:00Z"),
			turns: 1, lineage: "root", interrupted: true, versions: []string{"2.1.168"}, contextTokens: 15,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.sid[:8], func(t *testing.T) {
			got := parseFixture(t, tc.sid)
			w := tc.want
			if got.ID != tc.sid {
				t.Errorf("ID = %q", got.ID)
			}
			check := func(name string, got, want any) {
				t.Helper()
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("%s = %v, want %v", name, got, want)
				}
			}
			check("Title", got.Title, w.title)
			check("TitleSource", got.TitleSource, w.source)
			check("Cwd", got.Cwd, w.cwd)
			check("WorkCwd", got.WorkCwd, w.workCwd)
			check("LastCwd", got.LastCwd, w.lastCwd)
			check("Branch", got.Branch, w.branch)
			check("Entrypoint", got.Entrypoint, w.entrypoint)
			check("FirstPrompt", got.FirstPrompt, w.first)
			check("LastPrompt", got.LastPrompt, w.last)
			check("LastAssistant", got.LastAssistant, w.assistant)
			check("CreatedAt", got.CreatedAt.UTC(), w.created)
			check("LastActive", got.LastActive.UTC(), w.lastActive)
			check("Turns", got.Turns, w.turns)
			check("Compactions", got.Compactions, w.compactions)
			check("PermMode", got.PermMode, w.permMode)
			check("CostUSD", got.CostUSD, w.cost)
			check("len(PRs)", len(got.PRs), w.prs)
			check("len(Artifacts)", len(got.Artifacts), w.artifacts)
			check("BridgeURL", got.BridgeURL, w.bridge)
			check("Headless", got.Headless, w.headless)
			check("Lineage", got.Lineage, w.lineage)
			check("Interrupted", got.Interrupted, w.interrupted)
			check("DanglingTool", got.DanglingTool, w.dangling)
			check("FilesChanged", got.FilesChanged, w.files)
			check("BgJobsLost", got.BgJobsLost, w.bgJobs)
			check("Versions", got.Versions, w.versions)
			check("ContextTokens", got.ContextTokens, w.contextTokens)
			check("ParseErrors", got.ParseErrors, w.parseErrors)
			check("IsWorktree", got.IsWorktree, w.isWorktree)
			check("RepoRoot", got.RepoRoot, w.repoRoot)
			check("Lines", got.Lines, countLines(t, got.Path))
			if got.Size <= 0 || got.MTime.IsZero() {
				t.Errorf("Size/MTime not filled: %d %v", got.Size, got.MTime)
			}
		})
	}
}

func TestParseNormalLinks(t *testing.T) {
	got := parseFixture(t, sidNormal)
	if len(got.PRs) != 1 || got.PRs[0].URL != "https://github.com/alice/app/pull/42" || got.PRs[0].Number != 42 || got.PRs[0].Title != "alice/app" {
		t.Errorf("PRs = %+v", got.PRs)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].URL != "https://claude.ai/public/artifacts/synth-frame-1" || got.Artifacts[0].Title != "Retry design notes" {
		t.Errorf("Artifacts = %+v", got.Artifacts)
	}
	if len(got.Cwds) != 1 || got.Cwds[0] != "/Users/alice/dev/app" {
		t.Errorf("Cwds = %v", got.Cwds)
	}
}

func TestParseUnknownTypesCountedNotFatal(t *testing.T) {
	path := filepath.Join(fixtureDir(t), "projects", projectName, sidNormal+".jsonl")
	p, off, err := parseFull(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Unknown != 1 || p.Types["synthetic-future-record"] != 1 {
		t.Errorf("Unknown = %d Types = %v", p.Unknown, p.Types)
	}
	if p.S.ParseErrors != 1 {
		t.Errorf("ParseErrors = %d, want 1 for the non-JSON line", p.S.ParseErrors)
	}
	fi, _ := os.Stat(path)
	if off != fi.Size() {
		t.Errorf("offset %d != size %d", off, fi.Size())
	}
}

func TestParseEnvelopeless(t *testing.T) {
	got := parseFixture(t, sidNoEnv)
	if got.ID != sidNoEnv {
		t.Errorf("ID should fall back to the file name, got %q", got.ID)
	}
	if got.Title != "Empty session with a title" || got.TitleSource != "ai-title" {
		t.Errorf("Title = %q (%s)", got.Title, got.TitleSource)
	}
	if got.PermMode != "acceptEdits" {
		t.Errorf("PermMode = %q", got.PermMode)
	}
	if got.Cwd != "" || got.WorkCwd != "" || got.Turns != 0 || got.FirstPrompt != "" {
		t.Errorf("unexpected conversational data: %+v", got)
	}
	if !got.LastActive.Equal(got.MTime) || !got.CreatedAt.Equal(got.MTime) {
		t.Errorf("timestamps should fall back to mtime: %v %v %v", got.CreatedAt, got.LastActive, got.MTime)
	}
	if got.Headless {
		t.Error("an envelope-less transcript has no prompt and is not headless")
	}
}

func TestParseBigToolResultLine(t *testing.T) {
	got := parseFixture(t, sidBigResult)
	if got.Lines != 8 || got.ParseErrors != 0 {
		t.Errorf("Lines = %d ParseErrors = %d", got.Lines, got.ParseErrors)
	}
	if got.DanglingTool != "" {
		t.Errorf("tool_result on the long line should resolve the tool_use, got %q", got.DanglingTool)
	}
}

func writeTemp(t *testing.T, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func envelope(sid, typ, extra string) string {
	return `{"parentUuid":null,"isSidechain":false,"cwd":"/tmp/synth","sessionId":"` + sid + `","version":"2.1.227","gitBranch":"main","type":"` + typ + `","uuid":"x","timestamp":"2026-03-01T00:00:00.000Z",` + extra + `}`
}

func TestParseHeadlessSDK(t *testing.T) {
	sid := "abababab-abab-4bab-8bab-abababababab"
	path := writeTemp(t, sid+".jsonl",
		`{"type":"mode","mode":"default","sessionId":"`+sid+`"}`,
		envelope(sid, "user", `"entrypoint":"sdk-cli","promptId":"p-1","message":{"role":"user","content":"hello"}`),
		envelope(sid, "user", `"entrypoint":"sdk-cli","promptId":"p-2","message":{"role":"user","content":"again"}`),
	)
	got, err := ParseTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Headless {
		t.Error("sdk-cli entrypoint must be headless even with mode records and two prompts")
	}
	if got.Turns != 2 {
		t.Errorf("Turns = %d", got.Turns)
	}
}

func TestParseSkipsInjectedPrompts(t *testing.T) {
	sid := "cdcdcdcd-cdcd-4dcd-8dcd-cdcdcdcdcdcd"
	path := writeTemp(t, sid+".jsonl",
		envelope(sid, "user", `"message":{"role":"user","content":"<system-reminder>injected</system-reminder>"}`),
		envelope(sid, "user", `"message":{"role":"user","content":"<ide_opened_file>x.go</ide_opened_file>"}`),
		envelope(sid, "user", `"isMeta":true,"message":{"role":"user","content":"meta text"}`),
		envelope(sid, "user", `"isSidechain":true,"message":{"role":"user","content":"sidechain text"}`),
		envelope(sid, "user", `"message":{"role":"user","content":[{"type":"text","text":"  real   prompt\nwith lines "},{"type":"image","source":{}}]}`),
		envelope(sid, "user", `"message":{"role":"user","content":"[Request interrupted by user]"}`),
	)
	got, err := ParseTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstPrompt != "real prompt with lines" || got.Turns != 1 {
		t.Errorf("FirstPrompt = %q Turns = %d", got.FirstPrompt, got.Turns)
	}
	if got.Title != "real prompt with lines" || got.TitleSource != "first-prompt" {
		t.Errorf("Title = %q (%s)", got.Title, got.TitleSource)
	}
	if !got.Interrupted {
		t.Error("Interrupted should be set by a trailing interrupt record")
	}
}

func TestParseTitlePrecedenceAndEmptyFile(t *testing.T) {
	sid := "efefefef-efef-4fef-8fef-efefefefefef"
	path := writeTemp(t, sid+".jsonl",
		envelope(sid, "user", `"message":{"role":"user","content":"first"}`),
		`{"type":"ai-title","aiTitle":"AI","sessionId":"`+sid+`"}`,
		`{"type":"agent-name","agentName":"Agent","sessionId":"`+sid+`"}`,
		`{"type":"custom-title","customTitle":"Custom","sessionId":"`+sid+`"}`,
		`{"type":"custom-title","customTitle":"","sessionId":"`+sid+`"}`,
	)
	got, err := ParseTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Agent" || got.TitleSource != "agent-name" {
		t.Errorf("clearing the custom title should fall back to agent-name, got %q (%s)", got.Title, got.TitleSource)
	}

	empty := filepath.Join(t.TempDir(), "00000000-0000-4000-8000-000000000000.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ParseTranscript(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "00000000-0000-4000-8000-000000000000" || got.Title != "(untitled)" || got.Lines != 0 {
		t.Errorf("empty file: %+v", got)
	}
	if _, err := ParseTranscript(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Error("missing file must return an error")
	}
}

func TestParseCompactSummaryNotFirstIsRoot(t *testing.T) {
	sid := "0a0a0a0a-0a0a-4a0a-8a0a-0a0a0a0a0a0a"
	path := writeTemp(t, sid+".jsonl",
		envelope(sid, "user", `"message":{"role":"user","content":"start"}`),
		envelope(sid, "user", `"isCompactSummary":true,"message":{"role":"user","content":"summary"}`),
		envelope(sid, "user", `"message":{"role":"user","content":"after"}`),
	)
	got, err := ParseTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lineage != "root" || got.Turns != 2 {
		t.Errorf("Lineage = %s Turns = %d", got.Lineage, got.Turns)
	}
}

func TestFlexTypes(t *testing.T) {
	var ft flexTime
	for _, in := range []string{`"2026-01-15T09:00:00.000Z"`, `1768467600000`, `1768467600`, `"1768467600000"`} {
		if err := json.Unmarshal([]byte(in), &ft); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if ft.IsZero() || ft.Year() != 2026 {
			t.Errorf("%s parsed to %v", in, ft.Time)
		}
	}
	if err := json.Unmarshal([]byte(`"garbage"`), &ft); err != nil || !ft.IsZero() {
		t.Errorf("garbage timestamp should decode to zero without error: %v %v", ft.Time, err)
	}
	var fi flexInt
	for _, in := range []string{`42`, `"42"`, `42.0`} {
		if err := json.Unmarshal([]byte(in), &fi); err != nil || fi != 42 {
			t.Errorf("%s -> %d %v", in, fi, err)
		}
	}
	var fs flexString
	for in, want := range map[string]string{`"a"`: "a", `7`: "7", `true`: "true", `{"x":1}`: "", `null`: ""} {
		if err := json.Unmarshal([]byte(in), &fs); err != nil || string(fs) != want {
			t.Errorf("%s -> %q %v", in, fs, err)
		}
	}
	if got := oneLine("  a \n\t b  c ", 0); got != "a b c" {
		t.Errorf("oneLine = %q", got)
	}
	if got := oneLine(strings.Repeat("x", 10), 4); got != "xxxx" {
		t.Errorf("oneLine truncate = %q", got)
	}
}

// bigTranscript writes a transcript of roughly the requested size and
// returns its path and line count. Every line is newline terminated.
func bigTranscript(t *testing.T, sid string, target int64) (string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), sid+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 4<<20)
	pad := strings.Repeat("synthesized filler text ", 80) // ~2 KB
	n := 0
	write := func(s string) {
		w.WriteString(s)
		w.WriteByte('\n')
		n++
	}
	write(`{"type":"mode","mode":"default","sessionId":"` + sid + `"}`)
	write(envelope(sid, "user", `"promptId":"p-1","message":{"role":"user","content":"the first prompt"}`))
	var size int64
	for i := 0; size < target; i++ {
		line := `{"parentUuid":"x","isSidechain":false,"cwd":"/tmp/synth","sessionId":"` + sid + `","version":"2.1.227","gitBranch":"main","type":"assistant","uuid":"a","timestamp":"2026-03-01T01:00:00.000Z","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"` + pad + `"}],"usage":{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4}}}`
		write(line)
		size += int64(len(line) + 1)
		if i%1000 == 999 {
			write(envelope(sid, "system", `"subtype":"compact_boundary"`))
		}
	}
	write(envelope(sid, "user", `"promptId":"p-2","message":{"role":"user","content":"the last prompt"}`))
	write(`{"type":"ai-title","aiTitle":"Big one","sessionId":"` + sid + `"}`)
	write(`{"parentUuid":"x","isSidechain":false,"cwd":"/tmp/synth","sessionId":"` + sid + `","version":"2.1.227","gitBranch":"main","type":"assistant","uuid":"z","timestamp":"2026-03-01T02:00:00.000Z","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"final answer"}],"usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":4}}}`)
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path, n
}

func TestParseLargeFileHeadTail(t *testing.T) {
	if testing.Short() {
		t.Skip("large file test skipped in short mode")
	}
	sid := "b1b1b1b1-b1b1-4b1b-8b1b-b1b1b1b1b1b1"
	path, lines := bigTranscript(t, sid, 100<<20)
	fi, _ := os.Stat(path)
	if fi.Size() < 100<<20 {
		t.Fatalf("fixture only %d bytes", fi.Size())
	}
	start := time.Now()
	p, off, err := parseFull(path)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parsed %d MB in %v", fi.Size()>>20, elapsed)
	if elapsed > time.Second {
		t.Errorf("100 MB parse took %v, want under 1s", elapsed)
	}
	got := p.S
	if got.ID != sid || got.FirstPrompt != "the first prompt" || got.LastPrompt != "the last prompt" {
		t.Errorf("head/tail fields: id=%q first=%q last=%q", got.ID, got.FirstPrompt, got.LastPrompt)
	}
	if got.Title != "Big one" || got.LastAssistant != "final answer" || got.ContextTokens != 60 {
		t.Errorf("tail fields: title=%q assistant=%q ctx=%d", got.Title, got.LastAssistant, got.ContextTokens)
	}
	if got.Lines != lines {
		t.Errorf("Lines = %d, want exact %d", got.Lines, lines)
	}
	if off != fi.Size() {
		t.Errorf("offset %d != size %d", off, fi.Size())
	}
	if !got.LastActive.Equal(ts("2026-03-01T02:00:00Z")) {
		t.Errorf("LastActive = %v", got.LastActive)
	}

	// Appending after a head+tail parse continues incrementally.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(envelope(sid, "user", `"promptId":"p-3","message":{"role":"user","content":"appended prompt"}`) + "\n")
	f.Close()
	off2, err := parseIncremental(path, p, off)
	if err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(path)
	if off2 != fi2.Size() || p.S.LastPrompt != "appended prompt" || p.S.Lines != lines+1 {
		t.Errorf("incremental after head/tail: off=%d size=%d last=%q lines=%d", off2, fi2.Size(), p.S.LastPrompt, p.S.Lines)
	}
}

func TestForwardLinesLongLineAndPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jsonl")
	long := strings.Repeat("y", 3*lineBufSize)
	content := "a\n" + long + "\nb\npartial"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	var got []int
	off, err := forwardLines(f, 0, int64(len(content)), func(line []byte) bool {
		got = append(got, len(line))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != len(long) || got[2] != 1 {
		t.Errorf("line lengths = %v", got)
	}
	if want := int64(len("a\n") + len(long) + 1 + len("b\n")); off != want {
		t.Errorf("offset = %d, want %d (partial line not consumed)", off, want)
	}
	// Resuming from the returned offset with a limit that ends mid line yields nothing.
	off2, _ := forwardLines(f, off, 3, func([]byte) bool { return true })
	if off2 != off {
		t.Errorf("partial resume advanced offset to %d", off2)
	}
}
