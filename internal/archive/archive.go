package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Names used inside RecallDir.
const (
	ArchiveDirName = "archive"
	TranscriptName = "transcript.jsonl"
	ManifestName   = "manifest.json"
	MirrorName     = "history.mirror.jsonl"
	OffsetName     = "history.offset"
	SidecarDirName = "sidecars"
)

// RetentionKey is the only settings.json key recall ever writes.
const RetentionKey = "cleanupPeriodDays"

// ManifestFile describes one archived file.
type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is written to RecallDir/archive/<sid>/manifest.json once the
// archive is complete.
type Manifest struct {
	SID         string         `json:"sid"`
	Source      string         `json:"source"`
	Linked      bool           `json:"linked"`
	Files       []ManifestFile `json:"files"`
	CompletedAt time.Time      `json:"completedAt"`
}

// Dir returns the archive directory for sid.
func Dir(p model.Paths, sid string) string {
	return filepath.Join(p.RecallDir, ArchiveDirName, safeName(sid))
}

func safeName(s string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(s)
}

// Archive stores the transcript of sess under RecallDir/archive/<sid>/ and
// returns the archive directory. The transcript is hard-linked when the
// file system allows it and copied otherwise. When withSidecars is set the
// session's sidecar directories are copied as well; callers that want that
// part detached can pass false here and call CopySidecars later.
func Archive(p model.Paths, sess *model.Session, withSidecars bool) (string, error) {
	if sess == nil || sess.ID == "" {
		return "", errors.New("archive: session has no id")
	}
	if sess.Path == "" {
		return "", errors.New("archive: session has no transcript path")
	}
	src, err := os.Stat(sess.Path)
	if err != nil {
		return "", fmt.Errorf("archive: transcript: %w", err)
	}
	if !src.Mode().IsRegular() {
		return "", fmt.Errorf("archive: %s is not a regular file", sess.Path)
	}
	dir := Dir(p, sess.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, TranscriptName)
	linked, err := linkOrCopy(sess.Path, dst)
	if err != nil {
		return "", err
	}
	files := []ManifestFile{}
	mf, err := describe(dir, dst)
	if err != nil {
		return "", err
	}
	files = append(files, mf)
	if withSidecars {
		extra, err := CopySidecars(p, sess)
		if err != nil {
			return "", err
		}
		files = append(files, extra...)
	}
	m := Manifest{SID: sess.ID, Source: sess.Path, Linked: linked, Files: files, CompletedAt: time.Now().UTC()}
	if err := writeManifest(dir, m); err != nil {
		return "", err
	}
	return dir, nil
}

// SidecarDirs lists the directories Claude Code keeps next to a transcript:
// the per-session directory under the project dir (subagents, tool results)
// and ClaudeDir/file-history/<sid> plus ClaudeDir/tool-results/<sid> when
// present. Only existing directories are returned.
func SidecarDirs(p model.Paths, sess *model.Session) []string {
	if sess == nil || sess.ID == "" {
		return nil
	}
	var cands []string
	if sess.Path != "" {
		cands = append(cands, filepath.Join(filepath.Dir(sess.Path), sess.ID))
	}
	cands = append(cands,
		filepath.Join(p.ClaudeDir, "file-history", sess.ID),
		filepath.Join(p.ClaudeDir, "tool-results", sess.ID),
	)
	var out []string
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			continue
		}
		seen[c] = true
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			out = append(out, c)
		}
	}
	return out
}

// CopySidecars copies every sidecar directory of sess into
// RecallDir/archive/<sid>/sidecars/<kind>/ and returns the manifest entries.
// It is safe to run after Archive, for example from a detached process, and
// it rewrites the manifest with the added files.
func CopySidecars(p model.Paths, sess *model.Session) ([]ManifestFile, error) {
	dir := Dir(p, sess.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var files []ManifestFile
	for _, sd := range SidecarDirs(p, sess) {
		kind := filepath.Base(filepath.Dir(sd))
		if strings.HasPrefix(kind, "-") || filepath.Dir(filepath.Dir(sd)) == p.ProjectsDir {
			kind = "session"
		}
		target := filepath.Join(dir, SidecarDirName, kind)
		added, err := copyTree(sd, target, dir)
		if err != nil {
			return nil, err
		}
		files = append(files, added...)
	}
	// Merge into the existing manifest when there is one so a detached
	// sidecar copy does not lose the transcript entry.
	if m, err := ReadManifest(p, sess.ID); err == nil && m != nil {
		merged := map[string]ManifestFile{}
		for _, f := range m.Files {
			merged[f.Path] = f
		}
		for _, f := range files {
			merged[f.Path] = f
		}
		m.Files = m.Files[:0]
		for _, f := range merged {
			m.Files = append(m.Files, f)
		}
		sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
		m.CompletedAt = time.Now().UTC()
		if err := writeManifest(dir, *m); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// ReadManifest loads the manifest for sid; (nil, nil) when missing.
func ReadManifest(p model.Paths, sid string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(Dir(p, sid), ManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("archive: manifest: %w", err)
	}
	return &m, nil
}

// HasArchive reports whether sid has an archived transcript and its path.
func HasArchive(p model.Paths, sid string) (string, bool) {
	if sid == "" {
		return "", false
	}
	path := filepath.Join(Dir(p, sid), TranscriptName)
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return "", false
	}
	return path, true
}

// Restore returns a transcript path that can be resumed from. When the
// session's own transcript still exists it is returned as is. Otherwise the
// archived copy is put back under ProjectsDir, but only after checking that
// no other <sid>.jsonl (including .orphaned-* and .superseded-* variants)
// exists anywhere under ProjectsDir.
func Restore(p model.Paths, sess *model.Session) (string, error) {
	if sess == nil || sess.ID == "" {
		return "", errors.New("archive: session has no id")
	}
	if sess.Path != "" {
		if st, err := os.Stat(sess.Path); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return sess.Path, nil
		}
	}
	src, ok := HasArchive(p, sess.ID)
	if !ok {
		if sess.ArchivePath != "" {
			if st, err := os.Stat(sess.ArchivePath); err == nil && st.Mode().IsRegular() {
				src, ok = sess.ArchivePath, true
			}
		}
	}
	if !ok {
		return "", fmt.Errorf("archive: no archive for %s", model.ShortID(sess.ID))
	}
	others, err := findTranscripts(p.ProjectsDir, sess.ID)
	if err != nil {
		return "", err
	}
	if len(others) > 0 {
		return "", fmt.Errorf("archive: refusing to restore %s: transcript already present at %s", model.ShortID(sess.ID), strings.Join(others, ", "))
	}
	target := sess.Path
	if target == "" {
		cwd := sess.WorkCwd
		if cwd == "" {
			cwd = sess.Cwd
		}
		if cwd == "" {
			return "", errors.New("archive: session has neither a path nor a cwd to restore into")
		}
		target = filepath.Join(p.ProjectsDir, encodeProjectDir(cwd), sess.ID+".jsonl")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	if err := copyFile(src, target); err != nil {
		return "", err
	}
	if m, err := ReadManifest(p, sess.ID); err == nil && m != nil {
		for _, f := range m.Files {
			if f.Path != TranscriptName {
				continue
			}
			sum, _, err := hashFile(target)
			if err != nil {
				return "", err
			}
			if sum != f.SHA256 {
				_ = os.Remove(target)
				return "", fmt.Errorf("archive: restored transcript checksum mismatch for %s", model.ShortID(sess.ID))
			}
		}
	}
	return target, nil
}

// findTranscripts walks projectsDir for any file whose name starts with
// "<sid>.jsonl" (plain, .orphaned-*, .superseded-*).
func findTranscripts(projectsDir, sid string) ([]string, error) {
	if projectsDir == "" {
		return nil, nil
	}
	var hits []string
	prefix := sid + ".jsonl"
	err := filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == projectsDir && os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() {
			// Only project dirs sit directly under projectsDir; do not
			// descend into per-session sidecar directories.
			if path != projectsDir && filepath.Dir(path) != projectsDir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), prefix) {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, err
	}
	return hits, nil
}

// encodeProjectDir mirrors scan.EncodeProjectDir without importing it.
func encodeProjectDir(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		b[i] = '-'
	}
	return string(b)
}

// linkFn is os.Link; tests replace it to exercise the copy fallback.
var linkFn = os.Link

// linkOrCopy hard-links src to dst, replacing a stale dst, and falls back
// to a copy when linking fails. It reports whether dst is a hard link.
func linkOrCopy(src, dst string) (bool, error) {
	if _, err := os.Stat(dst); err == nil {
		if same, _ := sameFile(src, dst); same {
			return true, nil
		}
		if err := os.Remove(dst); err != nil {
			return false, err
		}
	}
	if err := linkFn(src, dst); err == nil {
		return true, nil
	}
	if err := copyFile(src, dst); err != nil {
		return false, err
	}
	return false, nil
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}

// copyFile copies src to dst via a temp file and rename.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// copyTree copies every regular file under src into dst and returns the
// manifest entries relative to base.
func copyTree(src, dst, base string) ([]ManifestFile, error) {
	var files []ManifestFile
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		mf, err := describe(base, target)
		if err != nil {
			return err
		}
		files = append(files, mf)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func describe(base, path string) (ManifestFile, error) {
	sum, size, err := hashFile(path)
	if err != nil {
		return ManifestFile{}, err
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		rel = path
	}
	return ManifestFile{Path: filepath.ToSlash(rel), Size: size, SHA256: sum}, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(filepath.Join(dir, ManifestName), data, 0o600)
}

// writeAtomic writes data to path via temp + rename.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(e error) error {
		tmp.Close()
		_ = os.Remove(tmpName)
		return e
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
