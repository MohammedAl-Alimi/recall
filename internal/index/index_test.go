package index

import (
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

var fixedNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func freeze(t *testing.T) {
	t.Helper()
	old := now
	now = func() time.Time { return fixedNow }
	t.Cleanup(func() { now = old })
}

func ago(d time.Duration) time.Time { return fixedNow.Add(-d) }

const sid = "0123456789ab-0000-0000-0000-000000000000"

func TestDeriveMatrix(t *testing.T) {
	freeze(t)
	alive := func(status string) *model.Live {
		return &model.Live{PID: 4242, Alive: true, Status: status, SessionID: sid}
	}
	recent := ago(time.Hour)
	cases := []struct {
		name string
		sess *model.Session
		live *model.Live
		mux  []model.MuxInfo
		ret  int
		want model.State
	}{
		{"ghost wins", &model.Session{ID: sid, Ghost: true}, alive("busy"), nil, 30, model.StateGhost},
		{"waiting status", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive("waiting"), nil, 30, model.StateNeedsYou},
		{"waiting for", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: true, Status: "idle", WaitingFor: "permission"}, nil, 30, model.StateNeedsYou},
		{"live busy", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive("busy"), nil, 30, model.StateLiveBusy},
		{"live idle", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive("idle"), nil, 30, model.StateLiveIdle},
		{"live no status", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive(""), nil, 30, model.StateLiveIdle},
		{"kept by pane name", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive("busy"), []model.MuxInfo{{Socket: "recall", SessionName: "rc-01234567"}}, 30, model.StateKept},
		{"kept by pane pid", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive("busy"), []model.MuxInfo{{Socket: "recall", SessionName: "rc-other", PanePID: 4242}}, 30, model.StateKept},
		{"kept via live.Mux", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: true, Mux: &model.MuxInfo{SessionName: "rc-01234567"}}, nil, 30, model.StateKept},
		{"kept pane without live", &model.Session{ID: sid, Path: "/t", LastActive: recent}, nil, []model.MuxInfo{{SessionName: "rc-01234567"}}, 30, model.StateKept},
		{"waiting beats kept", &model.Session{ID: sid, Path: "/t", LastActive: recent}, alive("waiting"), []model.MuxInfo{{SessionName: "rc-01234567"}}, 30, model.StateNeedsYou},
		{"bg kind", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: true, Kind: "bg"}, nil, 30, model.StateBG},
		{"bg daemon alive", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: true, BG: &model.BGInfo{ShortID: "01234567", DaemonAlive: true}}, nil, 30, model.StateBG},
		{"bg daemon dead", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: false, BG: &model.BGInfo{ShortID: "01234567"}}, nil, 30, model.StateBGStale},
		{"bg kind not alive", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: false, Kind: "bg"}, nil, 30, model.StateBGStale},
		{"foreign live vscode", &model.Session{ID: sid, Path: "/t", Entrypoint: "claude-vscode", LastActive: recent}, alive("busy"), nil, 30, model.StateForeign},
		{"foreign entrypoint closed falls through", &model.Session{ID: sid, Path: "/t", Entrypoint: "claude-desktop", LastActive: recent}, nil, nil, 30, model.StateClosed},
		{"cli entrypoint live is not foreign", &model.Session{ID: sid, Path: "/t", Entrypoint: "cli", LastActive: recent}, alive("busy"), nil, 30, model.StateLiveBusy},
		{"archived transcript gone", &model.Session{ID: sid, Archived: true, ArchivePath: "/a", LastActive: ago(90 * 24 * time.Hour)}, nil, nil, 30, model.StateArchived},
		{"archived transcript present", &model.Session{ID: sid, Path: "/t", Archived: true, LastActive: recent}, nil, nil, 30, model.StateClosed},
		{"archived never stale", &model.Session{ID: sid, Path: "/t", Archived: true, LastActive: ago(400 * 24 * time.Hour)}, nil, nil, 30, model.StateClosed},
		{"headless", &model.Session{ID: sid, Path: "/t", Headless: true, LastActive: recent}, nil, nil, 30, model.StateHeadless},
		{"interrupted", &model.Session{ID: sid, Path: "/t", Interrupted: true, LastActive: recent}, nil, nil, 30, model.StateInterrupted},
		{"headless beats interrupted", &model.Session{ID: sid, Path: "/t", Headless: true, Interrupted: true, LastActive: recent}, nil, nil, 30, model.StateHeadless},
		{"stale at retention-2", &model.Session{ID: sid, Path: "/t", LastActive: ago(29 * 24 * time.Hour)}, nil, nil, 30, model.StateStale},
		{"not stale inside window", &model.Session{ID: sid, Path: "/t", LastActive: ago(27 * 24 * time.Hour)}, nil, nil, 30, model.StateClosed},
		{"stale uses default retention when unset", &model.Session{ID: sid, Path: "/t", LastActive: ago(29 * 24 * time.Hour)}, nil, nil, 0, model.StateStale},
		{"short retention", &model.Session{ID: sid, Path: "/t", LastActive: ago(4 * 24 * time.Hour)}, nil, nil, 5, model.StateStale},
		{"dead live falls to closed", &model.Session{ID: sid, Path: "/t", LastActive: recent}, &model.Live{Alive: false, Status: "busy"}, nil, 30, model.StateClosed},
		{"closed", &model.Session{ID: sid, Path: "/t", LastActive: recent}, nil, nil, 30, model.StateClosed},
		{"zero last active is closed", &model.Session{ID: sid, Path: "/t"}, nil, nil, 30, model.StateClosed},
		{"nil session", nil, nil, nil, 30, model.StateClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Derive(tc.sess, tc.live, tc.mux, tc.ret); got != tc.want {
				t.Errorf("Derive() = %q, want %q", got, tc.want)
			}
		})
	}
}

func fixtures() []*model.Session {
	return []*model.Session{
		{
			ID: "aaaa1111-0000-0000-0000-000000000000", Title: "Fix login bug", FirstPrompt: "the login page crashes",
			LastPrompt: "add a regression test", LastAssistant: "Done, tests pass.",
			WorkCwd: "/home/u/dev/webapp", Cwd: "/home/u/dev/webapp", Branch: "fix/login",
			LastActive: ago(2 * time.Hour), State: model.StateNeedsYou,
			PRs:  []model.Link{{URL: "https://github.com/o/r/pull/42", Number: 42}},
			Tags: []string{"urgent"}, Notes: "customer escalation",
		},
		{
			ID: "bbbb2222-0000-0000-0000-000000000000", Title: "Refactor scanner", FirstPrompt: "split scan.go",
			WorkCwd: "/home/u/dev/recall", Branch: "main", Label: "scanner-work",
			LastActive: ago(3 * 24 * time.Hour), State: model.StateKept, Pinned: true,
		},
		{
			ID: "cccc3333-0000-0000-0000-000000000000", Title: "Docs pass", FirstPrompt: "rewrite the README",
			WorkCwd: "/home/u/dev/recall/docs", RepoRoot: "/home/u/dev/recall", Branch: "docs",
			LastActive: ago(20 * 24 * time.Hour), State: model.StateClosed,
		},
		{
			ID: "dddd4444-0000-0000-0000-000000000000", Ghost: true, Title: "what is a flock",
			GhostPrompts: []string{"what is a flock"}, Cwd: "/home/u/dev/webapp",
			LastActive: ago(40 * 24 * time.Hour), State: model.StateGhost,
		},
		{
			ID: "eeee5555-0000-0000-0000-000000000000", Title: "Budget spreadsheet", WorkCwd: "/home/u/finance",
			Branch: "main", LastActive: ago(10 * 24 * time.Hour), State: model.StateLiveIdle, Tags: []string{"money", "Personal"},
		},
	}
}

func ids(list []*model.Session) string {
	var out []string
	for _, s := range list {
		out = append(out, s.ID[:4])
	}
	return strings.Join(out, ",")
}

func TestFilterFreeText(t *testing.T) {
	freeze(t)
	all := fixtures()
	cases := map[string]string{
		"":                    "aaaa,bbbb,cccc,dddd,eeee",
		"login":               "aaaa",
		"LOGIN":               "aaaa",
		"regression":          "aaaa",
		"tests pass":          "aaaa",
		"login recall":        "",
		"tests":               "aaaa",
		"webapp":              "aaaa,dddd",
		"fix/login":           "aaaa",
		"aaaa1":               "aaaa",
		"42":                  "aaaa",
		"#42":                 "aaaa",
		"escalation":          "aaaa",
		"urgent":              "aaaa",
		"personal":            "eeee",
		"scanner-work":        "bbbb",
		"flock":               "dddd",
		"recall":              "bbbb,cccc",
		"recall docs":         "cccc",
		"nothing-matches-xyz": "",
	}
	for q, want := range cases {
		t.Run(q, func(t *testing.T) {
			if got := ids(Filter(all, q, ScopeAll, "")); got != want {
				t.Errorf("Filter(%q) = %q, want %q", q, got, want)
			}
		})
	}
}

func TestFilterTokens(t *testing.T) {
	freeze(t)
	all := fixtures()
	cases := map[string]string{
		"state:needs_you":        "aaaa",
		"state:needs-you":        "aaaa",
		"state:kept":             "bbbb",
		"state:live":             "aaaa,bbbb,eeee",
		"state:waiting":          "aaaa",
		"state:ghost":            "dddd",
		"state:closed":           "cccc",
		"branch:main":            "bbbb,eeee",
		"branch:fix":             "aaaa",
		"project:webapp":         "aaaa,dddd",
		"project:recall":         "bbbb,cccc",
		"project:finance":        "eeee",
		"since:7d":               "aaaa,bbbb",
		"since:24h":              "aaaa",
		"since:30d":              "aaaa,bbbb,cccc,eeee",
		"since:1w":               "aaaa,bbbb",
		"pr:42":                  "aaaa",
		"pr:#42":                 "aaaa",
		"pr:7":                   "",
		"tag:urgent":             "aaaa",
		"tag:personal":           "eeee",
		"label:scanner":          "bbbb",
		"branch:main since:7d":   "bbbb",
		"state:live project:web": "aaaa",
		"since:bogus":            "",
		"state:":                 "",
	}
	for q, want := range cases {
		t.Run(q, func(t *testing.T) {
			if got := ids(Filter(all, q, ScopeAll, "")); got != want {
				t.Errorf("Filter(%q) = %q, want %q", q, got, want)
			}
		})
	}
}

func TestFilterScopes(t *testing.T) {
	freeze(t)
	all := fixtures()
	cases := []struct {
		scope Scope
		cwd   string
		want  string
	}{
		{ScopeAll, "/x", "aaaa,bbbb,cccc,dddd,eeee"},
		{ScopeLive, "", "aaaa,bbbb,eeee"},
		{ScopeKept, "", "bbbb"},
		{ScopePinned, "", "bbbb"},
		{ScopeGhosts, "", "dddd"},
		{ScopeProject, "/home/u/dev/webapp", "aaaa,dddd"},
		{ScopeProject, "/home/u/dev/recall", "bbbb,cccc"},
		{ScopeProject, "/home/u/dev/recall/docs", "bbbb,cccc"},
		{ScopeProject, "/home/u/dev/recall/internal", "bbbb,cccc"},
		{ScopeProject, "/home/u/dev", "aaaa,bbbb,cccc,dddd"},
		{ScopeProject, "/home/u/dev/webapp2", ""},
		{ScopeProject, "/nowhere", ""},
		{ScopeProject, "", "aaaa,bbbb,cccc,dddd,eeee"},
	}
	for _, tc := range cases {
		if got := ids(Filter(all, "", tc.scope, tc.cwd)); got != tc.want {
			t.Errorf("Filter(scope=%s cwd=%s) = %q, want %q", tc.scope, tc.cwd, got, tc.want)
		}
	}
	if got := ids(Filter(all, "login", ScopeProject, "/home/u/dev/recall")); got != "" {
		t.Errorf("scope and query combine: got %q", got)
	}
}

func TestFilterDoesNotMutate(t *testing.T) {
	all := fixtures()
	before := ids(all)
	Filter(all, "login", ScopeAll, "")
	if ids(all) != before {
		t.Fatal("Filter mutated its input")
	}
	if got := Filter(nil, "x", ScopeAll, ""); got == nil || len(got) != 0 {
		t.Fatalf("Filter(nil) = %v, want empty non-nil slice", got)
	}
}

func TestSort(t *testing.T) {
	freeze(t)
	list := []*model.Session{
		{ID: "old-closed", State: model.StateClosed, LastActive: ago(10 * time.Hour)},
		{ID: "new-closed", State: model.StateClosed, LastActive: ago(1 * time.Hour)},
		{ID: "pin-old", State: model.StateClosed, Pinned: true, LastActive: ago(50 * time.Hour)},
		{ID: "needs-old", State: model.StateNeedsYou, LastActive: ago(99 * time.Hour)},
		{ID: "pin-new", State: model.StateClosed, Pinned: true, LastActive: ago(5 * time.Hour)},
		{ID: "needs-new", State: model.StateNeedsYou, LastActive: ago(2 * time.Hour)},
		{ID: "needs-pinned", State: model.StateNeedsYou, Pinned: true, LastActive: ago(80 * time.Hour)},
		{ID: "live", State: model.StateLiveBusy, LastActive: ago(3 * time.Hour)},
	}
	Sort(list)
	var got []string
	for _, s := range list {
		got = append(got, s.ID)
	}
	want := "needs-pinned,needs-new,needs-old,pin-new,pin-old,new-closed,live,old-closed"
	if strings.Join(got, ",") != want {
		t.Errorf("Sort order = %s\nwant       %s", strings.Join(got, ","), want)
	}
}

func TestSortStable(t *testing.T) {
	same := ago(time.Hour)
	list := []*model.Session{
		{ID: "a", LastActive: same},
		{ID: "b", LastActive: same},
		{ID: "c", LastActive: same},
	}
	for i := 0; i < 3; i++ {
		Sort(list)
	}
	if list[0].ID != "a" || list[1].ID != "b" || list[2].ID != "c" {
		t.Errorf("Sort is not stable: %s %s %s", list[0].ID, list[1].ID, list[2].ID)
	}
	Sort(nil)
	Sort([]*model.Session{})
}

func TestSummary(t *testing.T) {
	cases := []struct {
		list []*model.Session
		want string
	}{
		{nil, "0 sessions · 0 live"},
		{[]*model.Session{{State: model.StateClosed}}, "1 session · 0 live"},
		{[]*model.Session{
			{State: model.StateNeedsYou}, {State: model.StateLiveBusy}, {State: model.StateKept},
			{State: model.StateBG}, {State: model.StateClosed}, {State: model.StateGhost},
		}, "6 sessions · 4 live · 1 waiting"},
		{[]*model.Session{{State: model.StateLiveIdle}, {State: model.StateStale}}, "2 sessions · 1 live"},
	}
	for _, tc := range cases {
		if got := Summary(tc.list); got != tc.want {
			t.Errorf("Summary = %q, want %q", got, tc.want)
		}
	}
	if strings.Contains(Summary(fixtures()), "—") {
		t.Error("summary must not contain an em dash")
	}
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"7d": 7 * 24 * time.Hour, "24h": 24 * time.Hour, "90m": 90 * time.Minute,
		"2w": 14 * 24 * time.Hour, "3": 3 * 24 * time.Hour,
	}
	for in, want := range cases {
		got, ok := parseSince(in)
		if !ok || got != want {
			t.Errorf("parseSince(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "x", "-1d", "d", "1.5d"} {
		if _, ok := parseSince(bad); ok {
			t.Errorf("parseSince(%q) accepted", bad)
		}
	}
}
