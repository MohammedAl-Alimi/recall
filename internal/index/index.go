package index

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Scope narrows the session list.
type Scope string

// Scopes cycled with Tab in the UI.
const (
	ScopeAll     Scope = "all"
	ScopeProject Scope = "project"
	ScopeLive    Scope = "live"
	ScopeKept    Scope = "kept"
	ScopePinned  Scope = "pinned"
	ScopeGhosts  Scope = "ghosts"
)

// Scopes lists every scope in cycling order.
var Scopes = []Scope{ScopeAll, ScopeProject, ScopeLive, ScopeKept, ScopePinned, ScopeGhosts}

// DefaultRetentionDays is the Claude Code default for cleanupPeriodDays and
// is used by Derive when the caller passes a non-positive retention.
const DefaultRetentionDays = 30

// now is swapped in tests.
var now = time.Now

// Derive computes the state of sess from its live info, tmux panes and the
// retention window.
//
// Precedence, first match wins:
//
//	ghost
//	live and waiting            => needs_you
//	live and kept in tmux       => kept
//	live and bg                 => bg
//	live and foreign entrypoint => foreign
//	live busy / idle
//	kept pane without live info => kept
//	bg with dead daemon         => bg_stale
//	archived, transcript gone   => archived
//	headless
//	interrupted
//	older than retention-2 days => stale
//	closed
func Derive(sess *model.Session, live *model.Live, mux []model.MuxInfo, retentionDays int) model.State {
	if sess == nil {
		return model.StateClosed
	}
	if sess.Ghost {
		return model.StateGhost
	}
	kept := muxMatch(sess.ID, live, mux)
	if live != nil && live.Alive {
		if isWaiting(live) {
			return model.StateNeedsYou
		}
		if kept {
			return model.StateKept
		}
		if live.Kind == "bg" || (live.BG != nil && live.BG.DaemonAlive) {
			return model.StateBG
		}
		if foreignEntrypoint(sess.Entrypoint) {
			return model.StateForeign
		}
		if strings.EqualFold(live.Status, "busy") {
			return model.StateLiveBusy
		}
		return model.StateLiveIdle
	}
	if kept {
		return model.StateKept
	}
	if live != nil && live.BG != nil && !live.BG.DaemonAlive {
		return model.StateBGStale
	}
	if live != nil && !live.Alive && live.Kind == "bg" {
		return model.StateBGStale
	}
	if sess.Archived && sess.Path == "" {
		return model.StateArchived
	}
	if sess.Headless {
		return model.StateHeadless
	}
	if sess.Interrupted {
		return model.StateInterrupted
	}
	if !sess.Archived && isStale(sess.LastActive, retentionDays) {
		return model.StateStale
	}
	return model.StateClosed
}

func isWaiting(live *model.Live) bool {
	if live == nil {
		return false
	}
	if live.WaitingFor != "" {
		return true
	}
	return strings.EqualFold(live.Status, "waiting")
}

func foreignEntrypoint(ep string) bool {
	switch ep {
	case "", "cli":
		return false
	}
	return true
}

// muxMatch reports whether a tmux pane on the recall socket belongs to sid.
func muxMatch(sid string, live *model.Live, mux []model.MuxInfo) bool {
	if live != nil && live.Mux != nil && live.Mux.SessionName != "" {
		return true
	}
	if sid == "" {
		return false
	}
	name := "rc-" + model.ShortID(sid)
	for _, m := range mux {
		if m.SessionName == name {
			return true
		}
		if live != nil && live.PID != 0 && m.PanePID != 0 && m.PanePID == live.PID {
			return true
		}
	}
	return false
}

func isStale(lastActive time.Time, retentionDays int) bool {
	if lastActive.IsZero() {
		return false
	}
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	window := retentionDays - 2
	if window < 1 {
		window = 1
	}
	cutoff := now().Add(-time.Duration(window) * 24 * time.Hour)
	return lastActive.Before(cutoff)
}

// Filter applies query and scope to all. cwd is the shell cwd used by the
// project scope.
//
// The query is split on whitespace. Tokens of the form key:value are
// structured filters (state:, branch:, project:, since:, pr:, tag:, label:);
// every other token is matched case-insensitively as a substring over the
// title, label, prompts, project basename, branch, id prefix, PR numbers,
// notes and tags. All tokens must match.
func Filter(all []*model.Session, query string, scope Scope, cwd string) []*model.Session {
	q := parseQuery(query)
	out := make([]*model.Session, 0, len(all))
	for _, s := range all {
		if s == nil {
			continue
		}
		if !inScope(s, scope, cwd) {
			continue
		}
		if !q.match(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

type query struct {
	words  []string
	state  []string
	branch []string
	proj   []string
	tags   []string
	labels []string
	prs    []int
	since  time.Time
}

func parseQuery(raw string) query {
	var q query
	for _, tok := range strings.Fields(raw) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok || key == "" || val == "" {
			q.words = append(q.words, strings.ToLower(tok))
			continue
		}
		val = strings.ToLower(val)
		switch strings.ToLower(key) {
		case "state", "s":
			q.state = append(q.state, val)
		case "branch", "b":
			q.branch = append(q.branch, val)
		case "project", "p", "proj":
			q.proj = append(q.proj, val)
		case "tag", "t":
			q.tags = append(q.tags, strings.TrimPrefix(val, "#"))
		case "label", "l":
			q.labels = append(q.labels, val)
		case "pr":
			n, err := strconv.Atoi(strings.TrimPrefix(val, "#"))
			if err != nil {
				q.words = append(q.words, val)
				continue
			}
			q.prs = append(q.prs, n)
		case "since":
			if d, ok := parseSince(val); ok {
				t := now().Add(-d)
				if q.since.IsZero() || t.After(q.since) {
					q.since = t
				}
			} else {
				q.words = append(q.words, strings.ToLower(tok))
			}
		default:
			q.words = append(q.words, strings.ToLower(tok))
		}
	}
	return q
}

// parseSince accepts 7d, 24h, 90m, 2w and plain digits (days).
func parseSince(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	unit := v[len(v)-1]
	num := v
	mult := 24 * time.Hour
	switch unit {
	case 'd':
		num = v[:len(v)-1]
	case 'h':
		num = v[:len(v)-1]
		mult = time.Hour
	case 'm':
		num = v[:len(v)-1]
		mult = time.Minute
	case 'w':
		num = v[:len(v)-1]
		mult = 7 * 24 * time.Hour
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * mult, true
}

func (q query) match(s *model.Session) bool {
	if len(q.state) > 0 && !matchAny(q.state, func(v string) bool {
		st := strings.ToLower(string(s.State))
		switch v {
		case "live", "running":
			return s.State.IsLive()
		case "waiting":
			return s.State == model.StateNeedsYou
		}
		return st == v || strings.ReplaceAll(st, "_", "-") == v || strings.ReplaceAll(st, "_", "") == v
	}) {
		return false
	}
	if len(q.branch) > 0 && !matchAny(q.branch, func(v string) bool {
		return strings.Contains(strings.ToLower(s.Branch), v) ||
			strings.Contains(strings.ToLower(s.WorktreeBranch), v)
	}) {
		return false
	}
	if len(q.proj) > 0 && !matchAny(q.proj, func(v string) bool {
		for _, dir := range projectDirs(s) {
			if strings.Contains(strings.ToLower(filepath.Base(dir)), v) ||
				strings.Contains(strings.ToLower(dir), v) {
				return true
			}
		}
		return false
	}) {
		return false
	}
	if len(q.tags) > 0 && !matchAny(q.tags, func(v string) bool {
		for _, t := range s.Tags {
			if strings.ToLower(t) == v {
				return true
			}
		}
		return false
	}) {
		return false
	}
	if len(q.labels) > 0 && !matchAny(q.labels, func(v string) bool {
		return strings.Contains(strings.ToLower(s.Label), v)
	}) {
		return false
	}
	if len(q.prs) > 0 {
		found := false
		for _, n := range q.prs {
			for _, pr := range s.PRs {
				if pr.Number == n {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	if !q.since.IsZero() && s.LastActive.Before(q.since) {
		return false
	}
	for _, w := range q.words {
		if !freeTextMatch(s, w) {
			return false
		}
	}
	return true
}

func matchAny(vals []string, f func(string) bool) bool {
	for _, v := range vals {
		if f(v) {
			return true
		}
	}
	return false
}

func projectDirs(s *model.Session) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range append([]string{s.WorkCwd, s.Cwd, s.LastCwd, s.RepoRoot}, s.Cwds...) {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// freeTextMatch reports whether w (already lower-cased) occurs in any of the
// searchable text fields of s.
func freeTextMatch(s *model.Session, w string) bool {
	if w == "" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(s.ID), w) {
		return true
	}
	fields := []string{s.Title, s.Label, s.FirstPrompt, s.LastPrompt, s.LastAssistant, s.Branch, s.WorktreeBranch, s.Notes, s.Slug}
	for _, d := range projectDirs(s) {
		fields = append(fields, filepath.Base(d))
	}
	fields = append(fields, s.Tags...)
	fields = append(fields, s.GhostPrompts...)
	for _, f := range fields {
		if f != "" && strings.Contains(strings.ToLower(f), w) {
			return true
		}
	}
	num := strings.TrimPrefix(w, "#")
	if n, err := strconv.Atoi(num); err == nil {
		for _, pr := range s.PRs {
			if pr.Number == n {
				return true
			}
		}
	}
	for _, pr := range s.PRs {
		if strings.Contains(strings.ToLower(pr.URL), w) || strings.Contains(strings.ToLower(pr.Title), w) {
			return true
		}
	}
	return false
}

func inScope(s *model.Session, scope Scope, cwd string) bool {
	switch scope {
	case "", ScopeAll:
		return true
	case ScopeProject:
		return inProject(s, cwd)
	case ScopeLive:
		return s.State.IsLive()
	case ScopeKept:
		return s.State == model.StateKept
	case ScopePinned:
		return s.Pinned
	case ScopeGhosts:
		return s.Ghost
	}
	return true
}

// inProject reports whether s belongs to the project rooted at cwd: one of
// its working directories equals cwd, lies below cwd, or is an ancestor of
// cwd (limited to the repo root when the session knows one).
func inProject(s *model.Session, cwd string) bool {
	cwd = filepath.Clean(cwd)
	if cwd == "" || cwd == "." {
		return true
	}
	sep := string(filepath.Separator)
	root := ""
	if s.RepoRoot != "" {
		root = filepath.Clean(s.RepoRoot)
	}
	for _, d := range projectDirs(s) {
		d = filepath.Clean(d)
		if d == cwd || strings.HasPrefix(d, cwd+sep) {
			return true
		}
		if strings.HasPrefix(cwd, d+sep) {
			if root == "" || cwd == root || strings.HasPrefix(cwd, root+sep) {
				return true
			}
		}
	}
	return false
}

// Sort orders list in place: needs_you first, then pinned, then LastActive
// descending. The sort is stable so equal sessions keep their input order.
func Sort(list []*model.Session) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a == nil || b == nil {
			return a != nil
		}
		an, bn := a.State == model.StateNeedsYou, b.State == model.StateNeedsYou
		if an != bn {
			return an
		}
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		return a.LastActive.After(b.LastActive)
	})
}

// Summary returns the header line, e.g. "53 sessions · 13 live · 1 waiting".
func Summary(list []*model.Session) string {
	total, live, waiting := 0, 0, 0
	for _, s := range list {
		if s == nil {
			continue
		}
		total++
		if s.State.IsLive() {
			live++
		}
		if s.State == model.StateNeedsYou {
			waiting++
		}
	}
	parts := []string{fmt.Sprintf("%d %s", total, plural(total, "session", "sessions"))}
	parts = append(parts, fmt.Sprintf("%d live", live))
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", waiting))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
