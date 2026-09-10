package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

const (
	// headCap bounds the forward head read of a large transcript.
	headCap int64 = 8 << 20
	// tailCap bounds the backward tail read of a large transcript.
	tailCap int64 = 4 << 20
	// incrementalCap bounds how many appended bytes are parsed incrementally
	// before falling back to a fresh head+tail parse.
	incrementalCap int64 = 16 << 20

	maxPromptLen    = 500
	maxTitleLen     = 120
	maxFilesChanged = 500
	maxCwds         = 50
)

// metaTypes are metadata record types recall understands (or deliberately
// ignores). Anything else is counted in parser.Unknown.
var metaTypes = map[string]bool{
	"user": true, "assistant": true, "attachment": true, "system": true,
	"ai-title": true, "custom-title": true, "agent-name": true, "last-prompt": true,
	"mode": true, "permission-mode": true, "cost-state": true, "pr-link": true,
	"frame-link": true, "file-history-snapshot": true, "file-history-delta": true,
	"queue-operation": true, "bridge-session": true, "summary": true,
	"atis-latch": true, "artifact-autoreact-ledger": true, "artifact-comment-monitor": true,
	"progress": true, "result": true,
}

// skipPrefixes mark user records that are injected by the CLI rather than
// typed by the user; they never qualify as prompts or titles.
var skipPrefixes = []string{
	"<command-name>", "<local-command-caveat>", "<ide_opened_file>", "<system-reminder>",
	"<local-command-stdout>", "<local-command-stderr>", "<bash-input>", "<bash-stdout>",
	"<bash-stderr>", "<task-notification>", "<user-prompt-submit-hook>", "<command-message>",
	"[Request interrupted by user",
}

// editTools are tool names whose file_path input counts as a changed file.
var editTools = map[string]bool{
	"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true,
}

type toolUse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// parser accumulates state while records are applied in file order. It is
// JSON serialisable so an incremental re-scan can pick up where the last
// one stopped. Session fields derived from the accumulated state are
// (re)computed by finish, which is idempotent.
type parser struct {
	S *model.Session `json:"session"`

	AITitle     string `json:"ai_title,omitempty"`
	CustomTitle string `json:"custom_title,omitempty"`
	AgentName   string `json:"agent_name,omitempty"`
	Summary     string `json:"summary,omitempty"`
	HasMode     bool   `json:"has_mode,omitempty"`
	HasAITitle  bool   `json:"has_ai_title,omitempty"`

	PromptIDs    int    `json:"prompt_ids,omitempty"`
	LastPromptID string `json:"last_prompt_id,omitempty"`

	CwdCounts map[string]int `json:"cwd_counts,omitempty"`
	LastCwdAt time.Time      `json:"last_cwd_at,omitempty"`

	Pending      []toolUse `json:"pending,omitempty"`
	BgJobs       int       `json:"bg_jobs,omitempty"`
	SeenConv     bool      `json:"seen_conv,omitempty"`
	CompactFirst bool      `json:"compact_first,omitempty"`
	Interrupted  bool      `json:"interrupted,omitempty"`

	Unknown int            `json:"unknown,omitempty"`
	Types   map[string]int `json:"types,omitempty"`
}

func newParser(path string) *parser {
	return &parser{
		S:         &model.Session{Path: path},
		CwdCounts: map[string]int{},
		Types:     map[string]int{},
	}
}

// applyLine decodes one transcript line and folds it into the parser.
func (p *parser) applyLine(line []byte) {
	p.S.Lines++
	if len(line) == 0 || isBlank(line) {
		return
	}
	var rec rawRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		p.S.ParseErrors++
		return
	}
	p.apply(&rec)
}

func isBlank(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

func (p *parser) apply(rec *rawRecord) {
	s := p.S
	if rec.Type == "" {
		p.S.ParseErrors++
		return
	}
	p.Types[rec.Type]++
	if !metaTypes[rec.Type] {
		p.Unknown++
	}

	// Envelope fields are present on every conversational record.
	if rec.SessionID != "" {
		if s.ID == "" {
			s.ID = string(rec.SessionID)
		}
	}
	if !rec.Timestamp.IsZero() {
		ts := rec.Timestamp.Time
		if s.CreatedAt.IsZero() || ts.Before(s.CreatedAt) {
			s.CreatedAt = ts
		}
		if ts.After(s.LastActive) {
			s.LastActive = ts
		}
	}
	if rec.Version != "" {
		appendUnique(&s.Versions, string(rec.Version), 0)
	}
	if rec.GitBranch != "" {
		s.Branch = string(rec.GitBranch)
	}
	if rec.Entrypoint != "" && s.Entrypoint == "" {
		s.Entrypoint = string(rec.Entrypoint)
	}
	if rec.SessionKind != "" {
		s.Kind = string(rec.SessionKind)
	}
	if rec.Slug != "" {
		s.Slug = string(rec.Slug)
	}
	if rec.Cwd != "" {
		cwd := string(rec.Cwd)
		if s.Cwd == "" {
			s.Cwd = cwd
		}
		s.LastCwd = cwd
		appendUnique(&s.Cwds, cwd, maxCwds)
		if rec.Type == "user" || rec.Type == "assistant" {
			p.CwdCounts[cwd]++
		}
	}

	switch rec.Type {
	case "user":
		p.applyUser(rec)
	case "assistant":
		p.applyAssistant(rec)
	case "system":
		if rec.Subtype == "compact_boundary" {
			s.Compactions++
		}
	case "attachment":
		if rec.Attachment != nil && rec.Attachment.Type == "remote_session_change" && rec.Attachment.URL != "" {
			s.BridgeURL = string(rec.Attachment.URL)
		}
	case "ai-title":
		p.HasAITitle = true
		if rec.AITitle != "" {
			p.AITitle = oneLine(string(rec.AITitle), maxTitleLen)
		}
	case "custom-title":
		p.CustomTitle = oneLine(string(rec.CustomTitle), maxTitleLen)
	case "agent-name":
		p.AgentName = oneLine(string(rec.AgentName), maxTitleLen)
	case "summary":
		if rec.Summary != "" {
			p.Summary = oneLine(string(rec.Summary), maxTitleLen)
		}
	case "last-prompt":
		if rec.LastPrompt != "" {
			s.LastPrompt = oneLine(string(rec.LastPrompt), maxPromptLen)
		}
	case "mode":
		p.HasMode = true
	case "permission-mode":
		if rec.PermissionMode != "" {
			s.PermMode = string(rec.PermissionMode)
		}
	case "cost-state":
		if rec.TotalCostUSD > 0 {
			s.CostUSD = rec.TotalCostUSD
		}
	case "pr-link":
		url := firstNonEmpty(string(rec.PrURL), string(rec.URL))
		if url != "" {
			num := int(rec.PrNumber)
			if num == 0 {
				num = int(rec.Number)
			}
			addLink(&s.PRs, model.Link{URL: url, Number: num, Title: firstNonEmpty(string(rec.PrRepository), string(rec.Repo))})
		}
	case "frame-link":
		url := firstNonEmpty(string(rec.FrameURL), string(rec.URL))
		if url != "" {
			addLink(&s.Artifacts, model.Link{URL: url, Title: oneLine(string(rec.Title), maxTitleLen)})
		}
	}
}

func (p *parser) applyUser(rec *rawRecord) {
	s := p.S
	if rec.PromptID != "" && string(rec.PromptID) != p.LastPromptID {
		p.PromptIDs++
		p.LastPromptID = string(rec.PromptID)
	}
	if rec.IsSidechain {
		return
	}
	if rec.IsCompactSummary {
		if !p.SeenConv {
			p.CompactFirst = true
		}
		p.SeenConv = true
		return
	}
	p.SeenConv = true
	if rec.Message == nil {
		return
	}
	blocks, ok := contentBlocks(rec.Message.Content)
	if !ok {
		s.ParseErrors++
		return
	}
	if hasBlock(blocks, "tool_result") {
		for _, b := range blocks {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				p.resolveTool(string(b.ToolUseID))
			}
		}
		return
	}
	text := strings.TrimSpace(textOf(blocks))
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "[Request interrupted by user") {
		p.Interrupted = true
		p.Pending = nil
		return
	}
	if rec.IsMeta || !qualifies(text) {
		return
	}
	// A real typed prompt.
	p.Interrupted = false
	p.Pending = nil
	p.BgJobs = 0
	s.Turns++
	line := oneLine(text, maxPromptLen)
	if s.FirstPrompt == "" {
		s.FirstPrompt = line
	}
	s.LastPrompt = line
}

func (p *parser) applyAssistant(rec *rawRecord) {
	s := p.S
	if rec.IsSidechain || rec.Message == nil {
		return
	}
	p.SeenConv = true
	p.Interrupted = false
	blocks, ok := contentBlocks(rec.Message.Content)
	if !ok {
		s.ParseErrors++
		return
	}
	if rec.Message.Usage != nil {
		u := rec.Message.Usage
		if total := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens; total > 0 {
			s.ContextTokens = total
		}
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(string(b.Text)); t != "" {
				s.LastAssistant = oneLine(t, maxPromptLen)
			}
		case "tool_use":
			name := string(b.Name)
			p.Pending = append(p.Pending, toolUse{ID: string(b.ID), Name: name})
			if len(p.Pending) > 64 {
				p.Pending = p.Pending[len(p.Pending)-64:]
			}
			if editTools[name] || name == "Bash" {
				var in rawToolInput
				if len(b.Input) > 0 && json.Unmarshal(b.Input, &in) == nil {
					if editTools[name] {
						if fp := firstNonEmpty(string(in.FilePath), string(in.NotebookPath)); fp != "" {
							appendUnique(&s.FilesChanged, fp, maxFilesChanged)
						}
					}
					if name == "Bash" && in.RunInBackground {
						p.BgJobs++
					}
				}
			}
		}
	}
}

func (p *parser) resolveTool(id string) {
	for i, t := range p.Pending {
		if t.ID == id {
			p.Pending = append(p.Pending[:i], p.Pending[i+1:]...)
			return
		}
	}
}

// finish derives the presentation fields from the accumulated state. It may
// be called repeatedly (after every incremental parse).
func (p *parser) finish(fi os.FileInfo) {
	s := p.S
	if s.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
	}
	if fi != nil {
		s.Size = fi.Size()
		s.MTime = fi.ModTime()
		s.Inode = inodeOf(fi)
		if s.LastActive.IsZero() {
			s.LastActive = fi.ModTime()
		}
		if s.CreatedAt.IsZero() {
			s.CreatedAt = s.LastActive
		}
	}

	// Title precedence: custom-title > agent-name > ai-title > legacy summary > first prompt.
	switch {
	case p.CustomTitle != "":
		s.Title, s.TitleSource = p.CustomTitle, "custom-title"
	case p.AgentName != "":
		s.Title, s.TitleSource = p.AgentName, "agent-name"
	case p.AITitle != "":
		s.Title, s.TitleSource = p.AITitle, "ai-title"
	case p.Summary != "":
		s.Title, s.TitleSource = p.Summary, "summary"
	case s.FirstPrompt != "":
		s.Title, s.TitleSource = oneLine(s.FirstPrompt, maxTitleLen), "first-prompt"
	default:
		s.Title, s.TitleSource = "(untitled)", "none"
	}

	// WorkCwd: the cwd most conversational records were written from; ties
	// go to the most recently seen one.
	s.WorkCwd = ""
	best := 0
	for _, cwd := range s.Cwds {
		if n := p.CwdCounts[cwd]; n > best || (n == best && n > 0) {
			best, s.WorkCwd = n, cwd
		}
	}
	if s.WorkCwd == "" {
		s.WorkCwd = firstNonEmpty(s.LastCwd, s.Cwd)
	}
	s.RepoRoot, s.IsWorktree = repoInfo(s.WorkCwd)
	if s.IsWorktree {
		s.WorktreeBranch = s.Branch
	} else {
		s.WorktreeBranch = ""
	}

	s.Headless = s.Entrypoint == "sdk-cli" ||
		(!p.HasAITitle && !p.HasMode && p.CustomTitle == "" && p.PromptIDs == 1)
	if p.CompactFirst {
		s.Lineage = "continuation"
	} else {
		s.Lineage = "root"
	}
	s.Interrupted = p.Interrupted
	s.DanglingTool = ""
	if n := len(p.Pending); n > 0 {
		s.DanglingTool = p.Pending[n-1].Name
	}
	s.BgJobsLost = p.BgJobs
}

// repoInfo derives the repository root and worktree flag from a cwd. A
// Claude Code worktree lives under <repo>/.claude/worktrees/<name>. For other
// directories the nearest ancestor containing .git is used when it exists on
// disk; otherwise the root is unknown and left empty.
func repoInfo(cwd string) (root string, worktree bool) {
	if cwd == "" {
		return "", false
	}
	const marker = "/.claude/worktrees/"
	if i := strings.Index(cwd, marker); i > 0 {
		return cwd[:i], true
	}
	dir := cwd
	for i := 0; i < 32; i++ {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// qualifies reports whether a user text is a typed prompt rather than an
// injected CLI record.
func qualifies(text string) bool {
	for _, pre := range skipPrefixes {
		if strings.HasPrefix(text, pre) {
			return false
		}
	}
	return true
}

// oneLine collapses whitespace to single spaces and truncates to max runes.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		r := []rune(s)
		if len(r) > max {
			s = string(r[:max])
		}
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func appendUnique(list *[]string, v string, cap int) {
	for _, x := range *list {
		if x == v {
			return
		}
	}
	if cap > 0 && len(*list) >= cap {
		return
	}
	*list = append(*list, v)
}

func addLink(list *[]model.Link, l model.Link) {
	for i, x := range *list {
		if x.URL == l.URL {
			if l.Title != "" {
				(*list)[i].Title = l.Title
			}
			if l.Number != 0 {
				(*list)[i].Number = l.Number
			}
			return
		}
	}
	*list = append(*list, l)
}

// ParseTranscript parses a single transcript file into a Session. Files up
// to headCap+tailCap bytes are streamed fully; larger files are parsed from
// a head window (until the envelope and first typed prompt are known, at
// most headCap) plus a tail window (at most tailCap) so a 100 MB transcript
// still parses in well under a second. Unknown record types are counted,
// malformed lines increment ParseErrors; neither is fatal.
func ParseTranscript(path string) (*model.Session, error) {
	p, _, err := parseFull(path)
	if err != nil {
		return nil, err
	}
	return p.S, nil
}

// parseFull parses path from scratch and returns the parser plus the offset
// just past the last complete line it consumed.
func parseFull(path string) (*parser, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := fi.Size()
	p := newParser(path)

	if size <= headCap+tailCap {
		off, err := forwardLines(f, 0, size, func(line []byte) bool {
			p.applyLine(line)
			return true
		})
		if err != nil {
			return nil, 0, fmt.Errorf("scan %s: %w", path, err)
		}
		p.finish(fi)
		return p, off, nil
	}

	// Large file: bounded head, skipped middle, bounded tail.
	headEnd, err := forwardLines(f, 0, headCap, func(line []byte) bool {
		p.applyLine(line)
		return !(p.S.ID != "" && p.S.Cwd != "" && p.S.FirstPrompt != "")
	})
	if err != nil {
		return nil, 0, fmt.Errorf("scan %s: %w", path, err)
	}
	off, err := parseTail(f, p, headEnd, size)
	if err != nil {
		return nil, 0, fmt.Errorf("scan %s: %w", path, err)
	}
	p.finish(fi)
	return p, off, nil
}

// parseTail applies the last tailCap bytes of [headEnd, size) to p and
// returns the new offset. Lines skipped in the middle are counted so
// Session.Lines stays exact.
func parseTail(f *os.File, p *parser, headEnd, size int64) (int64, error) {
	tailStart := size - tailCap
	if tailStart < headEnd {
		tailStart = headEnd
	}
	lineStart, lines, consumed, err := tailLines(f, tailStart, size, tailStart == headEnd)
	if err != nil {
		return headEnd, err
	}
	skipped, err := countNewlines(f, headEnd, lineStart)
	if err != nil {
		return headEnd, err
	}
	p.S.Lines += skipped
	for _, line := range lines {
		p.applyLine(line)
	}
	if consumed < headEnd {
		consumed = headEnd
	}
	return consumed, nil
}

// parseIncremental applies the bytes of path between offset and the current
// end of file to a previously saved parser. The caller guarantees the file
// was only appended to since offset.
func parseIncremental(path string, p *parser, offset int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return offset, err
	}
	size := fi.Size()
	if p.CwdCounts == nil {
		p.CwdCounts = map[string]int{}
	}
	if p.Types == nil {
		p.Types = map[string]int{}
	}
	var off int64
	if size-offset <= incrementalCap {
		off, err = forwardLines(f, offset, size-offset, func(line []byte) bool {
			p.applyLine(line)
			return true
		})
	} else {
		off, err = parseTail(f, p, offset, size)
	}
	if err != nil {
		return offset, fmt.Errorf("scan %s: %w", path, err)
	}
	p.finish(fi)
	return off, nil
}
