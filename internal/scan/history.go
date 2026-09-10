package scan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// maxGhostPrompts caps the prompts kept per ghost session.
const maxGhostPrompts = 50

// historyLine is one entry of history.jsonl.
type historyLine struct {
	Display   flexString `json:"display"`
	Timestamp flexTime   `json:"timestamp"`
	Project   flexString `json:"project"`
	SessionID flexString `json:"sessionId"`
}

type ghostAcc struct {
	sess    *model.Session
	hasText bool
}

// Ghosts returns sessions that appear in history.jsonl but have no
// transcript. known maps session ids that already have a transcript.
// Sessions whose prompts are all slash commands are dropped.
func (s *Scanner) Ghosts(known map[string]bool) ([]*model.Session, error) {
	f, err := os.Open(s.Paths.HistoryFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	acc := map[string]*ghostAcc{}
	var order []string
	_, err = forwardLines(f, 0, fi.Size(), func(line []byte) bool {
		if len(line) == 0 || isBlank(line) {
			return true
		}
		var h historyLine
		if json.Unmarshal(line, &h) != nil || h.SessionID == "" {
			return true
		}
		sid := string(h.SessionID)
		if known[sid] {
			return true
		}
		g, ok := acc[sid]
		if !ok {
			g = &ghostAcc{sess: &model.Session{ID: sid, Ghost: true, State: model.StateGhost, Lineage: "root"}}
			acc[sid] = g
			order = append(order, sid)
		}
		gs := g.sess
		if h.Project != "" {
			proj := string(h.Project)
			if gs.Cwd == "" {
				gs.Cwd = proj
			}
			gs.LastCwd = proj
			gs.WorkCwd = proj
			appendUnique(&gs.Cwds, proj, maxCwds)
		}
		ts := h.Timestamp.Time
		if !ts.IsZero() {
			if gs.CreatedAt.IsZero() || ts.Before(gs.CreatedAt) {
				gs.CreatedAt = ts
			}
			if ts.After(gs.LastActive) {
				gs.LastActive = ts
			}
		}
		prompt := oneLine(string(h.Display), maxPromptLen)
		if prompt == "" {
			return true
		}
		if len(gs.GhostPrompts) < maxGhostPrompts {
			gs.GhostPrompts = append(gs.GhostPrompts, prompt)
		}
		gs.Turns++
		gs.LastPrompt = prompt
		if !isSlashCommand(prompt) {
			g.hasText = true
			if gs.FirstPrompt == "" {
				gs.FirstPrompt = prompt
			}
			if len(prompt) > len(gs.Title) {
				gs.Title = prompt
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	var out []*model.Session
	for _, sid := range order {
		g := acc[sid]
		if !g.hasText {
			continue
		}
		if s.transcriptExists(g.sess) {
			continue
		}
		g.sess.Title = oneLine(g.sess.Title, maxTitleLen)
		g.sess.TitleSource = "history"
		g.sess.RepoRoot, g.sess.IsWorktree = repoInfo(g.sess.WorkCwd)
		out = append(out, g.sess)
	}
	return out, nil
}

// transcriptExists checks the projects dir for a transcript that the caller
// did not list as known (for instance one written since the last Scan).
func (s *Scanner) transcriptExists(sess *model.Session) bool {
	for _, cwd := range sess.Cwds {
		p := filepath.Join(s.Paths.ProjectsDir, EncodeProjectDir(cwd), sess.ID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func isSlashCommand(prompt string) bool {
	p := strings.TrimSpace(prompt)
	return strings.HasPrefix(p, "/")
}
