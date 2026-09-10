package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/fsnotify/fsnotify"
)

// Ensure fsnotify stays in go.mod; a future implementation uses it to
// watch the projects dir for changes between rescans.
var _ = fsnotify.NewWatcher

// cacheSchema is bumped whenever the parser state layout changes so stale
// cache files are discarded instead of misread.
const cacheSchema = 1

// Stats counts what the last Scan did; useful for tests and doctor output.
type Stats struct {
	Files       int
	Cached      int
	Incremental int
	Full        int
	Failed      int
}

// Scanner discovers transcripts under Paths.ProjectsDir and caches parse
// results under Paths.RecallDir.
type Scanner struct {
	Paths model.Paths

	mu       sync.Mutex
	cache    map[string]*cacheEntry
	loaded   bool
	stats    Stats
	cacheErr error
}

type cacheFile struct {
	Schema  int                    `json:"schema"`
	Entries map[string]*cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Inode  uint64  `json:"inode"`
	Size   int64   `json:"size"`
	MTime  int64   `json:"mtime_ns"`
	Offset int64   `json:"offset"`
	P      *parser `json:"parser"`
}

// New returns a Scanner for the given paths.
func New(p model.Paths) *Scanner {
	return &Scanner{Paths: p, cache: map[string]*cacheEntry{}}
}

// Stats returns counters from the most recent Scan.
func (s *Scanner) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// CacheError returns the error of the last cache write, if any. Cache
// failures never fail a Scan; they only cost a re-parse next time.
func (s *Scanner) CacheError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cacheErr
}

func (s *Scanner) cachePath() string { return filepath.Join(s.Paths.RecallDir, "cache.json") }

// Scan returns every transcript-backed session (no ghosts). It uses the cache
// and parses incrementally from Session.Offset when possible.
func (s *Scanner) Scan(ctx context.Context) ([]*model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		s.loadCache()
		s.loaded = true
	}
	files, err := s.listTranscripts()
	if err != nil {
		return nil, err
	}
	stats := Stats{Files: len(files)}

	type job struct {
		path, projectDir string
	}
	type result struct {
		path       string
		projectDir string
		entry      *cacheEntry
		kind       int // 0 cached, 1 incremental, 2 full, 3 failed
	}
	jobs := make(chan job)
	results := make(chan result, len(files))
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				entry, kind := s.parseOne(j.path, s.cache[j.path])
				results <- result{path: j.path, projectDir: j.projectDir, entry: entry, kind: kind}
			}
		}()
	}
	sent := 0
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		jobs <- job{f.path, f.projectDir}
		sent++
	}
	close(jobs)
	wg.Wait()
	close(results)

	next := make(map[string]*cacheEntry, len(files))
	var out []*model.Session
	for r := range results {
		switch r.kind {
		case 0:
			stats.Cached++
		case 1:
			stats.Incremental++
		case 2:
			stats.Full++
		default:
			stats.Failed++
			continue
		}
		next[r.path] = r.entry
		sess := *r.entry.P.S
		sess.Offset = r.entry.Offset
		sess.ProjectDir = r.projectDir
		out = append(out, &sess)
	}
	if ctx.Err() != nil {
		// Keep entries for files we did not get to so their cache survives.
		for _, f := range files {
			if _, ok := next[f.path]; !ok {
				if e, ok := s.cache[f.path]; ok {
					next[f.path] = e
				}
			}
		}
	}
	s.cache = next
	s.stats = stats
	s.cacheErr = s.saveCache()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActive.Equal(out[j].LastActive) {
			return out[i].LastActive.After(out[j].LastActive)
		}
		return out[i].Path < out[j].Path
	})
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, nil
}

type transcriptFile struct {
	path, projectDir string
}

// listTranscripts finds ProjectsDir/*/<name>.jsonl, skipping orphaned and
// superseded copies and anything nested deeper (subagent transcripts).
func (s *Scanner) listTranscripts() ([]transcriptFile, error) {
	dirs, err := os.ReadDir(s.Paths.ProjectsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read projects dir: %w", err)
	}
	var out []transcriptFile
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		projectDir := filepath.Join(s.Paths.ProjectsDir, d.Name())
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !isTranscriptName(e.Name()) {
				continue
			}
			out = append(out, transcriptFile{path: filepath.Join(projectDir, e.Name()), projectDir: projectDir})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// isTranscriptName accepts <id>.jsonl and rejects orphaned/superseded
// copies, dot files and anything else.
func isTranscriptName(name string) bool {
	if !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.Contains(name, ".orphaned-") || strings.Contains(name, ".superseded-") {
		return false
	}
	stem := strings.TrimSuffix(name, ".jsonl")
	return stem != "" && !strings.Contains(stem, ".")
}

// parseOne returns the cache entry for path, reusing, extending or replacing
// the previous entry. kind: 0 cached, 1 incremental, 2 full, 3 failed.
func (s *Scanner) parseOne(path string, prev *cacheEntry) (*cacheEntry, int) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, 3
	}
	ino := inodeOf(fi)
	mt := fi.ModTime().UnixNano()
	if prev != nil && prev.P != nil && prev.P.S != nil {
		if prev.Inode == ino && prev.Size == fi.Size() && prev.MTime == mt {
			return prev, 0
		}
		if prev.Inode == ino && fi.Size() >= prev.Size && prev.Offset > 0 && prev.Offset <= fi.Size() && appendedOnly(path, prev.Offset) {
			off, err := parseIncremental(path, prev.P, prev.Offset)
			if err == nil {
				return &cacheEntry{Inode: ino, Size: fi.Size(), MTime: mt, Offset: off, P: prev.P}, 1
			}
		}
	}
	p, off, err := parseFull(path)
	if err != nil {
		return nil, 3
	}
	return &cacheEntry{Inode: ino, Size: fi.Size(), MTime: mt, Offset: off, P: p}, 2
}

// appendedOnly checks that the byte before offset is still a newline, the
// cheap signal that the file was appended to rather than rewritten.
func appendedOnly(path string, offset int64) bool {
	if offset <= 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [1]byte
	if _, err := f.ReadAt(b[:], offset-1); err != nil {
		return false
	}
	return b[0] == '\n'
}

func (s *Scanner) loadCache() {
	data, err := os.ReadFile(s.cachePath())
	if err != nil {
		return
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil || cf.Schema != cacheSchema {
		return
	}
	for k, e := range cf.Entries {
		if e == nil || e.P == nil || e.P.S == nil {
			continue
		}
		if e.P.CwdCounts == nil {
			e.P.CwdCounts = map[string]int{}
		}
		if e.P.Types == nil {
			e.P.Types = map[string]int{}
		}
		s.cache[k] = e
	}
}

func (s *Scanner) saveCache() error {
	if s.Paths.RecallDir == "" {
		return errors.New("recall dir not set")
	}
	if err := os.MkdirAll(s.Paths.RecallDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(cacheFile{Schema: cacheSchema, Entries: s.cache})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Paths.RecallDir, ".cache-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.cachePath())
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
