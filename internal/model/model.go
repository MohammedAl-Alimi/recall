package model

import "time"

// State is the derived lifecycle state of a session as shown in the UI.
type State string

// Session states. The raw string values are stable and used in --json output
// and in search tokens such as state:kept.
const (
	StateNeedsYou    State = "needs_you"
	StateLiveIdle    State = "live_idle"
	StateLiveBusy    State = "live_busy"
	StateKept        State = "kept"
	StateBG          State = "bg"
	StateBGStale     State = "bg_stale"
	StateClosed      State = "closed"
	StateInterrupted State = "interrupted"
	StateStale       State = "stale"
	StateArchived    State = "archived"
	StateGhost       State = "ghost"
	StateForeign     State = "foreign"
	StateHeadless    State = "headless"
)

// Word returns the short user-facing word for a state. It is always painted
// as plain text next to the color dot so the state is readable without color.
func (s State) Word() string {
	switch s {
	case StateNeedsYou:
		return "Needs you"
	case StateLiveIdle, StateLiveBusy, StateKept, StateBG, StateForeign:
		return "Running"
	case StateGhost, StateStale, StateBGStale:
		return "Gone"
	case StateClosed, StateInterrupted, StateArchived, StateHeadless:
		return "Closed"
	default:
		return "Closed"
	}
}

// IsLive reports whether the state means a claude process is currently running.
func (s State) IsLive() bool {
	switch s {
	case StateNeedsYou, StateLiveIdle, StateLiveBusy, StateKept, StateBG, StateForeign:
		return true
	}
	return false
}

// Link is a PR or artifact link found in a transcript.
type Link struct {
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
	Number int    `json:"number,omitempty"`
}

// Session is one Claude Code CLI session, either backed by a transcript file
// under the projects dir or, for ghosts, only by history.jsonl.
type Session struct {
	ID         string    `json:"id"`
	Path       string    `json:"path,omitempty"`
	ProjectDir string    `json:"project_dir,omitempty"`
	Inode      uint64    `json:"inode,omitempty"`
	Size       int64     `json:"size,omitempty"`
	MTime      time.Time `json:"mtime,omitempty"`
	Offset     int64     `json:"offset,omitempty"`

	Cwd     string   `json:"cwd,omitempty"`
	LastCwd string   `json:"last_cwd,omitempty"`
	WorkCwd string   `json:"work_cwd,omitempty"`
	Cwds    []string `json:"cwds,omitempty"`

	RepoRoot       string `json:"repo_root,omitempty"`
	IsWorktree     bool   `json:"is_worktree,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`

	Title       string `json:"title"`
	TitleSource string `json:"title_source,omitempty"`
	Label       string `json:"label,omitempty"`

	FirstPrompt   string `json:"first_prompt,omitempty"`
	LastPrompt    string `json:"last_prompt,omitempty"`
	LastAssistant string `json:"last_assistant,omitempty"`

	Branch     string   `json:"branch,omitempty"`
	Entrypoint string   `json:"entrypoint,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Slug       string   `json:"slug,omitempty"`
	Versions   []string `json:"versions,omitempty"`

	CreatedAt   time.Time `json:"created_at"`
	LastActive  time.Time `json:"last_active"`
	Turns       int       `json:"turns"`
	Compactions int       `json:"compactions,omitempty"`

	PermMode      string  `json:"perm_mode,omitempty"`
	ContextTokens int     `json:"context_tokens,omitempty"`
	CostUSD       float64 `json:"cost_usd,omitempty"`

	PRs       []Link `json:"prs,omitempty"`
	Artifacts []Link `json:"artifacts,omitempty"`
	BridgeURL string `json:"bridge_url,omitempty"`

	Headless     bool   `json:"headless,omitempty"`
	Lineage      string `json:"lineage,omitempty"`
	Interrupted  bool   `json:"interrupted,omitempty"`
	DanglingTool string `json:"dangling_tool,omitempty"`

	FilesChanged []string `json:"files_changed,omitempty"`
	BgJobsLost   int      `json:"bg_jobs_lost,omitempty"`

	Ghost        bool     `json:"ghost,omitempty"`
	GhostPrompts []string `json:"ghost_prompts,omitempty"`

	Archived    bool   `json:"archived,omitempty"`
	ArchivePath string `json:"archive_path,omitempty"`

	Live   *Live   `json:"live,omitempty"`
	Launch *Launch `json:"launch,omitempty"`
	State  State   `json:"state"`

	Pinned bool     `json:"pinned,omitempty"`
	Hidden bool     `json:"hidden,omitempty"`
	Notes  string   `json:"notes,omitempty"`
	Tags   []string `json:"tags,omitempty"`

	ParseErrors int `json:"parse_errors,omitempty"`
	Lines       int `json:"lines,omitempty"`
}

// Short returns the eight character prefix of the session id.
func (s *Session) Short() string { return ShortID(s.ID) }

// Live describes a running claude process bound to a session.
type Live struct {
	PID       int       `json:"pid"`
	ProcStart string    `json:"proc_start,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Alive     bool      `json:"alive"`

	SessionID  string `json:"session_id"`
	Name       string `json:"name,omitempty"`
	NameSource string `json:"name_source,omitempty"`
	Status     string `json:"status,omitempty"`
	WaitingFor string `json:"waiting_for,omitempty"`
	Kind       string `json:"kind,omitempty"`

	TTY     string   `json:"tty,omitempty"`
	HostApp string   `json:"host_app,omitempty"`
	Argv    []string `json:"argv,omitempty"`

	BridgeSessionID string   `json:"bridge_session_id,omitempty"`
	Mux             *MuxInfo `json:"mux,omitempty"`
	BG              *BGInfo  `json:"bg,omitempty"`
	Source          string   `json:"source,omitempty"`
}

// MuxInfo describes a tmux session kept by recall.
type MuxInfo struct {
	Socket      string `json:"socket"`
	SessionName string `json:"session_name"`
	Attached    bool   `json:"attached"`
	PanePID     int    `json:"pane_pid,omitempty"`
}

// BGInfo describes a background (claude --bg) session.
type BGInfo struct {
	ShortID     string `json:"short_id"`
	DaemonAlive bool   `json:"daemon_alive"`
}

// Launch records how a session was started so the same flags can be replayed
// on resume.
type Launch struct {
	SID         string    `json:"sid"`
	Argv        []string  `json:"argv"`
	Cwd         string    `json:"cwd"`
	TermProgram string    `json:"term_program,omitempty"`
	Mux         string    `json:"mux,omitempty"`
	RecordedBy  string    `json:"recorded_by,omitempty"`
	At          time.Time `json:"at"`
}

// ShortID returns the first 8 characters of a session id (or the whole id
// when shorter).
func ShortID(sid string) string {
	if len(sid) <= 8 {
		return sid
	}
	return sid[:8]
}
