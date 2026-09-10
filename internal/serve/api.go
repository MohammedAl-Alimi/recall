package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// maxFilesChanged bounds the filesChanged list per session row.
const maxFilesChanged = 8

// maxLabelLen bounds a label set through the API.
const maxLabelLen = 80

// Link mirrors the PR link shape of 'recall ls --json'.
type Link struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// LiveInfo mirrors the live block of 'recall ls --json'.
type LiveInfo struct {
	PID     int    `json:"pid"`
	Status  string `json:"status"`
	HostApp string `json:"hostApp"`
	TTY     string `json:"tty"`
}

// Row is one session in the API payload. The first block is the field set
// of 'recall ls --json' with the same keys; the rest is dashboard extras.
type Row struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	TitleSource   string    `json:"titleSource"`
	Label         string    `json:"label"`
	State         string    `json:"state"`
	StateWord     string    `json:"stateWord"`
	Project       string    `json:"project"`
	WorkCwd       string    `json:"workCwd"`
	Cwd           string    `json:"cwd"`
	Branch        string    `json:"branch"`
	LastActive    time.Time `json:"lastActive"`
	CreatedAt     time.Time `json:"createdAt"`
	Turns         int       `json:"turns"`
	ContextTokens int       `json:"contextTokens"`
	PRs           []Link    `json:"prs"`
	Live          *LiveInfo `json:"live"`
	Interrupted   bool      `json:"interrupted"`
	Headless      bool      `json:"headless"`
	Ghost         bool      `json:"ghost"`
	Archived      bool      `json:"archived"`
	ParseErrors   int       `json:"parseErrors"`

	Short          string   `json:"short"`
	IsLive         bool     `json:"isLive"`
	LastPrompt     string   `json:"lastPrompt"`
	LastAssistant  string   `json:"lastAssistant"`
	Pinned         bool     `json:"pinned"`
	Hidden         bool     `json:"hidden"`
	Note           string   `json:"note"`
	FilesChanged   []string `json:"filesChanged"`
	IsWorktree     bool     `json:"isWorktree"`
	WorktreeBranch string   `json:"worktreeBranch"`
	ResumeCommand  string   `json:"resumeCommand"`
	Notes          string   `json:"notes"`
	Tags           []string `json:"tags"`
}

// Payload is the response of GET /api/sessions and POST /api/refresh.
type Payload struct {
	Summary        string    `json:"summary"`
	Total          int       `json:"total"`
	Running        int       `json:"running"`
	NeedsYou       int       `json:"needsYou"`
	Degraded       bool      `json:"degraded"`
	DegradedReason string    `json:"degradedReason"`
	RetentionSet   bool      `json:"retentionSet"`
	RetentionDays  int       `json:"retentionDays"`
	CmuxAvailable  bool      `json:"cmuxAvailable"`
	TermProgram    string    `json:"termProgram"`
	DryRun         bool      `json:"dryRun"`
	Warnings       []string  `json:"warnings"`
	LoadedAt       time.Time `json:"loadedAt"`
	Sessions       []Row     `json:"sessions"`
}

// OpenResult is the response of POST /api/open.
type OpenResult struct {
	OK          bool   `json:"ok"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Note        string `json:"note"`
	DryRun      bool   `json:"dryRun"`
	Target      string `json:"target"`
	TTY         string `json:"tty"`
}

func dryRun() bool { return os.Getenv("RECALL_DRY_RUN") == "1" }

func projectName(s *model.Session) string {
	for _, d := range []string{s.WorkCwd, s.LastCwd, s.Cwd} {
		if d != "" {
			return filepath.Base(d)
		}
	}
	return ""
}

// commandLine renders an action as the shell command a user could paste.
func commandLine(cwd string, argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	cmd := launch.ShellJoin(argv)
	// A multiplexer command carries the working directory in its own flags,
	// so prefixing 'cd' would show a command nobody runs.
	if cwd == "" || carriesOwnCwd(argv) {
		return cmd
	}
	return "cd " + launch.ShellQuote(cwd) + " && " + cmd
}

// carriesOwnCwd reports whether argv already tells its terminal which
// directory to start in, which cmux and tmux both do.
func carriesOwnCwd(argv []string) bool {
	for _, a := range argv {
		if a == "--cwd" || a == "-c" {
			return true
		}
	}
	return false
}

func toRow(s *model.Session) Row {
	r := Row{
		ID:            s.ID,
		Title:         s.Title,
		TitleSource:   s.TitleSource,
		Label:         s.Label,
		State:         string(s.State),
		StateWord:     s.State.Word(),
		Project:       projectName(s),
		WorkCwd:       s.WorkCwd,
		Cwd:           s.Cwd,
		Branch:        s.Branch,
		LastActive:    s.LastActive.UTC(),
		CreatedAt:     s.CreatedAt.UTC(),
		Turns:         s.Turns,
		ContextTokens: s.ContextTokens,
		PRs:           []Link{},
		Interrupted:   s.Interrupted,
		Headless:      s.Headless,
		Ghost:         s.Ghost,
		Archived:      s.Archived,
		ParseErrors:   s.ParseErrors,

		Short:          s.Short(),
		IsLive:         s.State.IsLive(),
		LastPrompt:     s.LastPrompt,
		LastAssistant:  s.LastAssistant,
		Pinned:         s.Pinned,
		Hidden:         s.Hidden,
		FilesChanged:   []string{},
		IsWorktree:     s.IsWorktree,
		WorktreeBranch: s.WorktreeBranch,
		Notes:          s.Notes,
		Tags:           []string{},
	}
	if r.Title == "" && len(s.GhostPrompts) > 0 {
		r.Title = s.GhostPrompts[0]
	}
	for _, l := range s.PRs {
		r.PRs = append(r.PRs, Link{URL: l.URL, Number: l.Number, Title: l.Title})
	}
	if s.Live != nil {
		r.Live = &LiveInfo{PID: s.Live.PID, Status: s.Live.Status, HostApp: s.Live.HostApp, TTY: s.Live.TTY}
	}
	for i, f := range s.FilesChanged {
		if i == maxFilesChanged {
			break
		}
		r.FilesChanged = append(r.FilesChanged, f)
	}
	if len(s.Tags) > 0 {
		r.Tags = append(r.Tags, s.Tags...)
	}
	if !s.Ghost {
		cwd, argv, note := launch.BuildResume(s, launch.Options{})
		r.ResumeCommand = commandLine(cwd, argv)
		if !r.IsLive {
			r.Note = note
		}
	}
	return r
}

// ensureFresh applies the staleness rules: a full Load after loadStaleAfter
// (or when never loaded), a live refresh after liveStaleAfter. Callers hold
// s.mu. When force is set a full Load always runs.
func (s *Server) ensureFresh(ctx context.Context, force bool) error {
	now := time.Now()
	switch {
	case force || !s.loaded || now.Sub(s.lastLoad) > loadStaleAfter:
		if err := s.app.Load(ctx, true); err != nil {
			return err
		}
		s.loaded = true
		s.lastLoad, s.lastLive = now, now
	case now.Sub(s.lastLive) > liveStaleAfter:
		if err := s.app.RefreshLive(ctx); err != nil {
			return err
		}
		s.lastLive = now
	}
	return nil
}

// payload builds the API payload for the given toggles. Callers hold s.mu.
func (s *Server) payload(showHidden, showGhosts, showHeadless bool) Payload {
	a := s.app
	list := a.Visible(showHidden, showGhosts, showHeadless)
	p := Payload{
		Degraded:       a.Degraded,
		DegradedReason: a.DegradedReason,
		RetentionSet:   a.RetentionSet,
		RetentionDays:  a.RetentionDays,
		TermProgram:    os.Getenv("TERM_PROGRAM"),
		DryRun:         dryRun(),
		Warnings:       append([]string{}, a.Warnings...),
		LoadedAt:       a.LoadedAt.UTC(),
		Sessions:       make([]Row, 0, len(list)),
	}
	_, p.CmuxAvailable = launch.CmuxAvailable()
	for _, sess := range list {
		r := toRow(sess)
		p.Sessions = append(p.Sessions, r)
		if sess.Ghost {
			continue
		}
		p.Total++
		if r.IsLive {
			p.Running++
		}
		if sess.State == model.StateNeedsYou {
			p.NeedsYou++
		}
	}
	p.Summary = summaryLine(p.Total, p.Running, p.NeedsYou)
	return p
}

func summaryLine(total, running, needsYou int) string {
	parts := []string{fmt.Sprintf("%d %s", total, plural(total, "session", "sessions"))}
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", running))
	}
	if needsYou > 0 {
		parts = append(parts, fmt.Sprintf("%d %s you", needsYou, plural(needsYou, "needs", "need")))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func flag(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true"
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureFresh(r.Context(), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.payload(flag(r, "hidden"), flag(r, "ghosts"), flag(r, "headless")))
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureFresh(r.Context(), true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.payload(flag(r, "hidden"), flag(r, "ghosts"), flag(r, "headless")))
}

type openReq struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Fork   bool   `json:"fork"`
}

// terminalFor maps an API target to launch.Options.Terminal.
func terminalFor(target string) (string, error) {
	switch target {
	case "", "newtab":
		return "", nil
	case "terminal", "iterm":
		return target, nil
	case launch.TerminalCmux:
		if _, ok := launch.CmuxAvailable(); !ok {
			return "", errors.New("cmux is not installed")
		}
		return launch.TerminalCmux, nil
	}
	return "", fmt.Errorf("unknown target %q", target)
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req openReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	term, err := terminalFor(req.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	opts := launch.Options{
		NewTab:      true,
		Fork:        req.Fork,
		Terminal:    term,
		TermProgram: os.Getenv("TERM_PROGRAM"),
	}

	s.mu.Lock()
	if err := s.ensureFresh(r.Context(), false); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sess, err := s.app.Find(req.ID)
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if sess.Ghost {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, sess.Short()+" has no transcript left to resume")
		return
	}
	act, err := s.app.Open(sess, opts)
	s.mu.Unlock()
	if err != nil {
		if strings.Contains(err.Error(), "already open") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	res := OpenResult{
		OK:          true,
		Kind:        act.Kind,
		Description: act.Description,
		Command:     commandLine(act.Cwd, act.Argv),
		Note:        act.Note,
		DryRun:      dryRun(),
		Target:      req.Target,
	}
	if act.Kind == launch.KindFocus && sess.Live != nil {
		res.TTY = filepath.Base(sess.Live.TTY)
	}
	if res.DryRun || act.Kind == launch.KindPrint {
		writeJSON(w, http.StatusOK, res)
		return
	}
	if act.Kind != launch.KindFocus && act.Script == "" && act.Terminal != launch.TerminalCmux {
		// Without a script or a multiplexer launch.Run would exec claude in
		// place of this server. Report the command instead so the user can
		// paste it. cmux runs as a child process, so it is safe here.
		s.mu.Lock()
		s.app.ReleaseLock(sess.ID)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, OpenResult{
			OK: false, Kind: act.Kind, Command: res.Command, Note: res.Note, Target: req.Target,
			Description: "no terminal backend for this action; run the command yourself",
		})
		return
	}
	s.runMu.Lock()
	err = launch.Run(act)
	s.runMu.Unlock()
	if err != nil {
		s.mu.Lock()
		s.app.ReleaseLock(sess.ID)
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type metaReq struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Pinned bool   `json:"pinned"`
	Hidden bool   `json:"hidden"`
}

// editMeta applies one meta change ("label", "pin" or "hide") to the
// session named in the request, persists meta.json and responds with the
// updated row.
func (s *Server) editMeta(w http.ResponseWriter, r *http.Request, kind string) {
	var req metaReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Label = strings.TrimSpace(strings.ReplaceAll(req.Label, "\n", " "))
	if kind == "label" && len([]rune(req.Label)) > maxLabelLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("label longer than %d characters", maxLabelLen))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureFresh(r.Context(), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sess, err := s.app.Find(req.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.app.EditMeta(func(m *state.Meta) {
		switch kind {
		case "label":
			m.SetLabel(sess.ID, req.Label)
		case "pin":
			if req.Pinned {
				m.Pin(sess.ID)
			} else {
				m.Unpin(sess.ID)
			}
		case "hide":
			if req.Hidden {
				m.Hide(sess.ID)
			} else {
				m.Unhide(sess.ID)
			}
		}
	})
	s.app.ApplyMeta()
	if err := s.app.SaveMeta(); err != nil {
		writeError(w, http.StatusInternalServerError, "save meta: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": toRow(sess)})
}

func (s *Server) handleLabel(w http.ResponseWriter, r *http.Request) { s.editMeta(w, r, "label") }
func (s *Server) handlePin(w http.ResponseWriter, r *http.Request)   { s.editMeta(w, r, "pin") }
func (s *Server) handleHide(w http.ResponseWriter, r *http.Request)  { s.editMeta(w, r, "hide") }
