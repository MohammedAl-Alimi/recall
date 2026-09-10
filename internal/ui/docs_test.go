package ui

// Tests that keep README.md and site/index.html in step with the UI: the
// mockup in both is a real NO_COLOR render of synthesized sessions, and the
// key tables are generated from actionTable. When one of these tests fails,
// regenerate the block it names with the helper it prints instead of editing
// the docs by hand.

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// mockupSessions are the five rows painted in the README and on the site.
// They are synthesized by hand and contain no real transcript data.
func mockupSessions() []*model.Session {
	return []*model.Session{
		{
			ID: "0f1e2d3c-1111-4222-8333-444444444444", Title: "Login page for the dashboard",
			WorkCwd: "/Users/me/dev/ascension", Cwd: "/Users/me/dev/ascension", Branch: "main",
			LastPrompt: "add the login page", LastAssistant: "Waiting for permission: Edit src/app/login.tsx",
			LastActive: fixedNow.Add(-2 * time.Minute), State: model.StateNeedsYou,
			Live: &model.Live{PID: 4242, Alive: true, Status: "waiting", HostApp: "Terminal.app", TTY: "ttys003"},
			PRs:  []model.Link{{URL: "https://github.com/x/y/pull/42", Number: 42}},
		},
		{
			ID: "1a2b3c4d-1111-4222-8333-444444444444", Title: "Scanner incremental cache",
			WorkCwd: "/Users/me/dev/recall", Cwd: "/Users/me/dev/recall", Branch: "feat/scan",
			IsWorktree: true, WorktreeBranch: "feat/scan", LastAssistant: "Running go test ./internal/scan ...",
			LastActive: fixedNow.Add(-20 * time.Second), State: model.StateLiveBusy,
			Live: &model.Live{PID: 4343, Alive: true, Status: "busy"},
		},
		{
			ID: "2b3c4d5e-1111-4222-8333-444444444444", Title: "Fix flaky calendar popover test",
			WorkCwd: "/Users/me/dev/lcc", Cwd: "/Users/me/dev/lcc", Branch: "main",
			LastAssistant: "Fixed the race by cancelling the previous timer.",
			LastActive:    fixedNow.Add(-3 * time.Hour), State: model.StateClosed,
		},
		{
			ID: "3c4d5e6f-1111-4222-8333-444444444444", Title: "ETF savings plan rebalance",
			WorkCwd: "/Users/me/dev/sparplan", Cwd: "/Users/me/dev/sparplan", Branch: "main",
			Lineage: "fork", LastAssistant: "Here is the drift table for September.",
			LastActive: fixedNow.Add(-2 * 24 * time.Hour), State: model.StateClosed,
		},
		{
			ID: "4d5e6f70-1111-4222-8333-444444444444", Title: "Kitchen forecast kickoff notes",
			WorkCwd: "/Users/me/dev/biergarten", Cwd: "/Users/me/dev/biergarten",
			Ghost: true, GhostPrompts: []string{"summarise the kickoff notes for the kitchen forecast"},
			LastActive: fixedNow.Add(-12 * 24 * time.Hour), State: model.StateGhost,
		},
	}
}

// mockupWidth and mockupHeight are the terminal size of the documented render.
const (
	mockupWidth  = 90
	mockupHeight = 20
)

// renderMockup returns the NO_COLOR screen for mockupSessions with trailing
// blanks removed from every line and blank lines at the end dropped.
func renderMockup(t *testing.T) string {
	t.Helper()
	m, fb := newTestModel(t)
	fb.sessions = mockupSessions()
	m.showGhosts = true
	m.width, m.height = mockupWidth, mockupHeight
	m.refreshRows()
	return trimRender(ansiRE.ReplaceAllString(m.View(), ""))
}

func trimRender(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// navKeys are cursor movements, which the docs fold into one sentence.
var navKeys = map[action]bool{actUp: true, actDown: true, actPageUp: true, actPageDown: true, actHome: true, actEnd: true}

// docKeys lists the actions that belong in the documented key tables: every
// bound, enabled action except cursor movement.
func docKeys() []actionInfo {
	var out []actionInfo
	for _, i := range actionTable {
		if i.key == "" || i.disabled || navKeys[i.act] {
			continue
		}
		out = append(out, i)
	}
	return out
}

// keyTableMarkdown is the README "Keys" table, generated from actionTable.
func keyTableMarkdown() string {
	var b strings.Builder
	b.WriteString("| Key | Action |\n| --- | --- |\n")
	for _, i := range docKeys() {
		b.WriteString("| `" + i.key + "` | " + i.label + " |\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// keyListHTML is the site "Keys" section body, generated from actionTable.
func keyListHTML() string {
	var b strings.Builder
	for _, i := range docKeys() {
		b.WriteString("        <div><kbd>" + html.EscapeString(i.key) + "</kbd> " + html.EscapeString(i.label) + "</div>\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fencedBlock returns the body of the first fenced code block in md that
// starts with prefix.
func fencedBlock(t *testing.T, md, prefix string) string {
	t.Helper()
	re := regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")
	for _, m := range re.FindAllStringSubmatch(md, -1) {
		if strings.HasPrefix(m[1], prefix) {
			return strings.TrimRight(m[1], "\n")
		}
	}
	t.Fatalf("no fenced block starting with %q", prefix)
	return ""
}

func stripTags(s string) string {
	return html.UnescapeString(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, ""))
}

func TestReadmeMockupIsRealRender(t *testing.T) {
	want := renderMockup(t)
	got := fencedBlock(t, repoFile(t, "README.md"), " recall  5 sessions")
	if got != want {
		t.Errorf("README mockup differs from the NO_COLOR render; paste this block:\n%s\n\nREADME has:\n%s", want, got)
	}
}

func TestReadmeKeyTableMatchesActionTable(t *testing.T) {
	want := keyTableMarkdown()
	md := repoFile(t, "README.md")
	start := strings.Index(md, "| Key | Action |")
	if start < 0 {
		t.Fatal("README has no key table")
	}
	rest := md[start:]
	end := strings.Index(rest, "\n\n")
	if end < 0 {
		end = len(rest)
	}
	got := strings.TrimRight(rest[:end], "\n")
	if got != want {
		t.Errorf("README key table differs from actionTable; paste this table:\n%s\n\nREADME has:\n%s", want, got)
	}
}

func TestSiteMockupIsRealRender(t *testing.T) {
	want := renderMockup(t)
	site := repoFile(t, filepath.Join("site", "index.html"))
	re := regexp.MustCompile(`(?s)<pre class="mockup">(.*?)</pre>`)
	m := re.FindStringSubmatch(site)
	if m == nil {
		t.Fatal(`site has no <pre class="mockup"> block`)
	}
	got := trimRender(stripTags(m[1]))
	if got != want {
		t.Errorf("site mockup differs from the NO_COLOR render; want:\n%s\n\nsite has:\n%s", want, got)
	}
}

func TestSiteKeysMatchActionTable(t *testing.T) {
	want := keyListHTML()
	site := repoFile(t, filepath.Join("site", "index.html"))
	re := regexp.MustCompile(`(?s)<div class="keys">\n(.*?)\n      </div>`)
	m := re.FindStringSubmatch(site)
	if m == nil {
		t.Fatal(`site has no <div class="keys"> block`)
	}
	got := strings.TrimRight(m[1], "\n")
	if got != want {
		t.Errorf("site keys differ from actionTable; paste this list:\n%s\n\nsite has:\n%s", want, got)
	}
}

// TestDocsStateGlyphs checks that the README states table paints the dots
// and color words the UI actually uses.
func TestDocsStateGlyphs(t *testing.T) {
	md := repoFile(t, "README.md")
	for _, st := range []model.State{model.StateNeedsYou, model.StateLiveIdle, model.StateClosed, model.StateGhost} {
		row := "| `" + stateDot(st) + "`"
		if !strings.Contains(md, row+" ") && !strings.Contains(md, row+"|") {
			t.Errorf("README states table has no row starting with %q for %s", row, st)
		}
	}
	if !strings.Contains(md, "| `●` red | Needs you |") {
		t.Errorf("README should paint Needs you as red, the color of styles.needsYou")
	}
}
