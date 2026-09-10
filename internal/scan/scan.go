package scan

import (
	"context"
	"errors"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/fsnotify/fsnotify"
)

// Ensure fsnotify stays in go.mod; a future implementation uses it to
// watch the projects dir for changes between rescans.
var _ = fsnotify.NewWatcher

// Scanner discovers transcripts under Paths.ProjectsDir and caches parse
// results under Paths.RecallDir.
type Scanner struct {
	Paths model.Paths

	cache map[string]*model.Session
}

// New returns a Scanner for the given paths.
func New(p model.Paths) *Scanner {
	return &Scanner{Paths: p, cache: map[string]*model.Session{}}
}

// Scan returns every transcript-backed session (no ghosts). It uses the cache
// and parses incrementally from Session.Offset when possible.
func (s *Scanner) Scan(ctx context.Context) ([]*model.Session, error) {
	return nil, errors.New("not implemented: scan.Scanner.Scan")
}

// Ghosts returns sessions that appear in history.jsonl but have no
// transcript. known maps session ids that already have a transcript.
func (s *Scanner) Ghosts(known map[string]bool) ([]*model.Session, error) {
	return nil, errors.New("not implemented: scan.Scanner.Ghosts")
}

// ParseTranscript parses a single transcript file into a Session.
func ParseTranscript(path string) (*model.Session, error) {
	return nil, errors.New("not implemented: scan.ParseTranscript")
}

// EncodeProjectDir converts a working directory into the directory name
// Claude Code uses under projects/: every byte outside [A-Za-z0-9] becomes '-'.
func EncodeProjectDir(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		default:
			b[i] = '-'
		}
	}
	return string(b)
}
