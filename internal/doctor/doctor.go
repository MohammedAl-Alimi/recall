package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/hook"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/shell"
)

// MinClaudeVersion is the lowest claude CLI version recall supports.
const MinClaudeVersion = "2.1.223"

// DefaultRetentionDays is what Claude Code uses when cleanupPeriodDays is
// not set.
const DefaultRetentionDays = 30

// Check is one doctor result.
type Check struct {
	Name string
	// Status is one of "ok", "warn", "fail".
	Status string
	Detail string
}

// Features lists the claude CLI capabilities detected from --help.
type Features struct {
	Version     string
	Resume      bool
	ForkSession bool
	SessionID   bool
	Name        bool
	BG          bool
	AgentsJSON  bool
	Attach      bool
}

// runClaude executes the claude binary with args and returns combined
// output. Tests replace it. Only --version and --help are ever passed.
var runClaude = func(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "claude", args...).CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// lookPath is exec.LookPath; tests replace it.
var lookPath = exec.LookPath

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// DetectFeatures parses 'claude --version' and 'claude --help'.
func DetectFeatures() (Features, error) {
	var f Features
	vout, err := runClaude("--version")
	if err != nil {
		return f, fmt.Errorf("claude --version: %w", err)
	}
	f.Version = versionRe.FindString(vout)
	if f.Version == "" {
		return f, fmt.Errorf("claude --version: cannot parse %q", strings.TrimSpace(vout))
	}
	hout, err := runClaude("--help")
	if err != nil {
		return f, fmt.Errorf("claude --help: %w", err)
	}
	f = parseHelp(hout, f)
	return f, nil
}

// parseHelp fills the feature flags from help text.
func parseHelp(help string, f Features) Features {
	for _, line := range strings.Split(help, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case hasFlag(t, "--resume"):
			f.Resume = true
		case hasFlag(t, "--fork-session"):
			f.ForkSession = true
		case hasFlag(t, "--session-id"):
			f.SessionID = true
		case hasFlag(t, "--name"):
			f.Name = true
		case hasFlag(t, "--bg") || hasFlag(t, "--background"):
			f.BG = true
		}
		if isCommand(t, "agents") {
			f.AgentsJSON = true
		}
		if isCommand(t, "attach") {
			f.Attach = true
		}
	}
	return f
}

// hasFlag reports whether a help line starts with the given long flag,
// optionally preceded by a short alias like "-r, ".
func hasFlag(line, flag string) bool {
	if !strings.HasPrefix(line, "-") {
		return false
	}
	for _, tok := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == ',' }) {
		if !strings.HasPrefix(tok, "-") {
			return false
		}
		if tok == flag {
			return true
		}
	}
	return false
}

// isCommand reports whether a help line documents a subcommand.
func isCommand(line, name string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != name {
		return false
	}
	rest := strings.TrimPrefix(line, name)
	if strings.HasPrefix(rest, "  ") {
		return true
	}
	return len(fields) > 1 && (strings.HasPrefix(fields[1], "[") || strings.HasPrefix(fields[1], "<"))
}

// CompareVersions returns -1, 0 or 1 comparing dotted numeric versions.
func CompareVersions(a, b string) int {
	pa, pb := splitVersion(a), splitVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func splitVersion(v string) [3]int {
	var out [3]int
	v = versionRe.FindString(v)
	for i, s := range strings.SplitN(v, ".", 3) {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(s)
		out[i] = n
	}
	return out
}

// Run executes every check. It never writes anything.
func Run(p model.Paths) ([]Check, error) {
	var checks []Check
	checks = append(checks, checkClaude())
	checks = append(checks, checkProjects(p))
	checks = append(checks, checkRegistry(p))
	checks = append(checks, checkRetention(p))
	checks = append(checks, checkTmux())
	checks = append(checks, checkTerminal())
	checks = append(checks, checkRecallDir(p))
	checks = append(checks, checkHooks(p))
	checks = append(checks, checkWidget())
	checks = append(checks, checkParser())
	checks = append(checks, Check{Name: "network", Status: "ok", Detail: "recall makes no network connections"})
	return checks, nil
}

// Failed reports whether any check has status fail.
func Failed(checks []Check) bool {
	for _, c := range checks {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}

func checkClaude() Check {
	f, err := DetectFeatures()
	if err != nil {
		return Check{Name: "claude", Status: "fail", Detail: "claude CLI not usable: " + err.Error()}
	}
	var missing []string
	for name, ok := range map[string]bool{"--resume": f.Resume, "--fork-session": f.ForkSession, "--session-id": f.SessionID, "--name": f.Name} {
		if !ok {
			missing = append(missing, name)
		}
	}
	extras := []string{}
	if f.BG {
		extras = append(extras, "bg")
	}
	if f.AgentsJSON {
		extras = append(extras, "agents")
	}
	if f.Attach {
		extras = append(extras, "attach")
	}
	detail := fmt.Sprintf("claude %s", f.Version)
	if len(extras) > 0 {
		detail += " (" + strings.Join(extras, ", ") + ")"
	}
	if CompareVersions(f.Version, MinClaudeVersion) < 0 {
		return Check{Name: "claude", Status: "warn", Detail: fmt.Sprintf("%s is older than %s; some features may be missing", detail, MinClaudeVersion)}
	}
	if len(missing) > 0 {
		return Check{Name: "claude", Status: "warn", Detail: detail + "; flags not found in --help: " + strings.Join(missing, ", ")}
	}
	return Check{Name: "claude", Status: "ok", Detail: detail}
}

func checkProjects(p model.Paths) Check {
	st, err := os.Stat(p.ProjectsDir)
	if err != nil || !st.IsDir() {
		return Check{Name: "projects", Status: "fail", Detail: "projects dir not found: " + p.ProjectsDir}
	}
	n, projects, err := CountTranscripts(p.ProjectsDir)
	if err != nil {
		return Check{Name: "projects", Status: "warn", Detail: "projects dir not readable: " + err.Error()}
	}
	return Check{Name: "projects", Status: "ok", Detail: fmt.Sprintf("%d transcripts in %d projects under %s", n, projects, p.ProjectsDir)}
}

// CountTranscripts counts <uuid>.jsonl transcripts directly under each
// project dir, skipping orphaned and superseded copies.
func CountTranscripts(projectsDir string) (transcripts, projects int, err error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projectsDir, e.Name()))
		if err != nil {
			continue
		}
		found := 0
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			found++
		}
		if found > 0 {
			projects++
			transcripts += found
		}
	}
	return transcripts, projects, nil
}

func checkRegistry(p model.Paths) Check {
	entries, err := os.ReadDir(p.SessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{Name: "registry", Status: "warn", Detail: "no live session registry at " + p.SessionsDir + " (liveness falls back to 'claude agents')"}
		}
		return Check{Name: "registry", Status: "warn", Detail: "registry not readable: " + err.Error()}
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return Check{Name: "registry", Status: "ok", Detail: fmt.Sprintf("%d registry entries in %s", n, p.SessionsDir)}
}

func checkRetention(p model.Paths) Check {
	days, set, err := archive.Retention(p)
	if err != nil {
		return Check{Name: "retention", Status: "warn", Detail: "cannot read settings.json: " + err.Error()}
	}
	if !set {
		return Check{Name: "retention", Status: "warn", Detail: fmt.Sprintf("cleanupPeriodDays not set: Claude Code deletes transcripts after %d days; run 'recall setup --retention 3650'", DefaultRetentionDays)}
	}
	if days < 365 {
		return Check{Name: "retention", Status: "warn", Detail: fmt.Sprintf("cleanupPeriodDays=%d; transcripts older than that are deleted by Claude Code", days)}
	}
	return Check{Name: "retention", Status: "ok", Detail: fmt.Sprintf("cleanupPeriodDays=%d", days)}
}

func checkTmux() Check {
	path, err := lookPath("tmux")
	if err != nil {
		return Check{Name: "tmux", Status: "warn", Detail: "tmux not installed; kept sessions (K) are unavailable"}
	}
	return Check{Name: "tmux", Status: "ok", Detail: "tmux at " + path}
}

func checkTerminal() Check {
	tp := os.Getenv("TERM_PROGRAM")
	switch tp {
	case "":
		return Check{Name: "terminal", Status: "warn", Detail: "TERM_PROGRAM not set; new tabs and focus need Terminal.app or iTerm2"}
	case "Apple_Terminal", "iTerm.app":
		return Check{Name: "terminal", Status: "ok", Detail: tp + " (tab focus and new tab supported)"}
	default:
		return Check{Name: "terminal", Status: "ok", Detail: tp + " (opens in place; no tab focus)"}
	}
}

func checkRecallDir(p model.Paths) Check {
	st, err := os.Stat(p.RecallDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{Name: "recall-dir", Status: "warn", Detail: p.RecallDir + " does not exist yet (created on first run)"}
		}
		return Check{Name: "recall-dir", Status: "fail", Detail: err.Error()}
	}
	if !st.IsDir() {
		return Check{Name: "recall-dir", Status: "fail", Detail: p.RecallDir + " is not a directory"}
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		return Check{Name: "recall-dir", Status: "warn", Detail: fmt.Sprintf("%s has mode %04o, want 0700", p.RecallDir, perm)}
	}
	return Check{Name: "recall-dir", Status: "ok", Detail: p.RecallDir + " (0700)"}
}

func checkHooks(p model.Paths) Check {
	installed, missing, err := HooksInstalled(p)
	if err != nil {
		return Check{Name: "hooks", Status: "warn", Detail: "cannot read settings.json: " + err.Error()}
	}
	if len(installed) == 0 {
		return Check{Name: "hooks", Status: "warn", Detail: "recall hooks not installed; run 'recall setup --hooks'"}
	}
	if len(missing) > 0 {
		return Check{Name: "hooks", Status: "warn", Detail: "hooks installed for " + strings.Join(installed, ", ") + "; missing " + strings.Join(missing, ", ")}
	}
	return Check{Name: "hooks", Status: "ok", Detail: "hooks installed for " + strings.Join(installed, ", ")}
}

// HooksInstalled reports which of recall's default hook events carry a
// recall command in settings.json.
func HooksInstalled(p model.Paths) (installed, missing []string, err error) {
	obj, err := archive.ReadSettings(p)
	if err != nil {
		return nil, nil, err
	}
	hooks, _ := obj["hooks"].(map[string]any)
	for _, ev := range hook.DefaultEvents {
		list, _ := hooks[ev].([]any)
		if hasRecallCommand(list) {
			installed = append(installed, ev)
		} else {
			missing = append(missing, ev)
		}
	}
	return installed, missing, nil
}

func hasRecallCommand(list []any) bool {
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, "/recall hook") || strings.HasPrefix(strings.TrimSpace(cmd), "recall hook") {
				return true
			}
		}
	}
	return false
}

func checkWidget() Check {
	sh, rc := shell.Detect()
	if sh == "" {
		return Check{Name: "widget", Status: "warn", Detail: "unsupported or unknown $SHELL; Ctrl-G widget not available"}
	}
	if shell.Installed(rc) {
		return Check{Name: "widget", Status: "ok", Detail: fmt.Sprintf("Ctrl-G widget installed in %s (%s)", rc, sh)}
	}
	return Check{Name: "widget", Status: "warn", Detail: fmt.Sprintf("Ctrl-G widget not installed in %s; run 'recall shell install'", rc)}
}

// Format renders checks as aligned text lines.
func Format(checks []Check) string {
	var b strings.Builder
	for _, c := range checks {
		mark := "ok  "
		switch c.Status {
		case "warn":
			mark = "warn"
		case "fail":
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "%s  %-11s %s\n", mark, c.Name, c.Detail)
	}
	return b.String()
}
