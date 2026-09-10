package index

import "github.com/MohammedAl-Alimi/recall/internal/model"

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

// Derive computes the state of sess from its live info, tmux panes and the
// retention window.
func Derive(sess *model.Session, live *model.Live, mux []model.MuxInfo, retentionDays int) model.State {
	return model.StateClosed
}

// Filter applies query and scope to all. cwd is the shell cwd used by the
// project scope.
func Filter(all []*model.Session, query string, scope Scope, cwd string) []*model.Session {
	return all
}

// Sort orders list in place: needs_you first, then pinned, then LastActive
// descending.
func Sort(list []*model.Session) {}

// Summary returns the header line, e.g. "53 sessions · 13 live · 1 waiting".
func Summary(list []*model.Session) string {
	return ""
}
