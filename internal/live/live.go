package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Source names recorded in model.Live.Source.
const (
	SourceAgentsJSON = "agents-json"
	SourceRegistry   = "registry"
)

// startedAtTolerance is how far the registry startedAt may drift from the
// process start reported by ps before the process is treated as reused.
const startedAtTolerance = 15 * time.Second

// agentsTimeout bounds the 'claude agents --json --all' call.
const agentsTimeout = 10 * time.Second

// errAgentsSkipped is returned by the agents hook when the command must not
// run (tests, RECALL_NO_AGENTS_JSON=1, or no claude binary on PATH).
var errAgentsSkipped = errors.New("agents json skipped")

// Prober discovers live claude processes.
type Prober struct {
	Paths model.Paths

	degraded bool
	reason   string

	// agents runs 'claude agents --json --all'. Tests replace it so no real
	// claude binary is ever executed. nil means the real command.
	agents func(ctx context.Context) ([]byte, error)

	// agentsErr is the error of the last agents call, for diagnostics.
	agentsErr error
}

// New returns a Prober for the given paths.
func New(p model.Paths) *Prober {
	return &Prober{Paths: p}
}

// AgentsError returns the error of the last 'claude agents --json' call, or
// nil when it succeeded or was skipped.
func (p *Prober) AgentsError() error {
	if errors.Is(p.agentsErr, errAgentsSkipped) {
		return nil
	}
	return p.agentsErr
}

// Degraded reports whether the last probe ran in degraded mode and why.
func (p *Prober) Degraded() (bool, string) {
	return p.degraded, p.reason
}

// runAgentsCommand executes 'claude agents --json --all' with a timeout.
func runAgentsCommand(ctx context.Context) ([]byte, error) {
	if os.Getenv("RECALL_NO_AGENTS_JSON") != "" {
		return nil, errAgentsSkipped
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil, errAgentsSkipped
	}
	ctx, cancel := context.WithTimeout(ctx, agentsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "agents", "--json", "--all")
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), fmt.Errorf("claude agents --json: %s", msg)
	}
	return out.Bytes(), nil
}

// fileStamp identifies a file version by inode and mtime so a rewrite of
// daemon.lock is distinguishable from an untouched one.
type fileStamp struct {
	exists bool
	size   int64
	mtime  time.Time
}

// String renders the stamp for diagnostics.
func (f fileStamp) String() string {
	if !f.exists {
		return "absent"
	}
	return fmt.Sprintf("%d bytes at %s", f.size, f.mtime.UTC().Format(time.RFC3339))
}

func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, size: st.Size(), mtime: st.ModTime()}
}

// candidate is the merged view of one pid before liveness verification.
type candidate struct {
	pid    int
	reg    *registryEntry
	agent  *agentEntry
	source string
}

// Probe returns live process info keyed by session id. Entries whose
// process is gone or reused are included with Alive=false so callers can
// tell "was live, now closed" from "never seen".
func (p *Prober) Probe(ctx context.Context) (map[string]*model.Live, error) {
	p.degraded = false
	p.reason = ""
	p.agentsErr = nil
	var reasons []string

	// Primary source: claude agents --json --all.
	lockPath := filepath.Join(p.Paths.ClaudeDir, "daemon.lock")
	before := stampOf(lockPath)
	run := p.agents
	if run == nil {
		run = runAgentsCommand
	}
	var agents []agentEntry
	raw, err := run(ctx)
	if err != nil {
		p.agentsErr = err
	} else {
		agents, err = parseAgentsJSON(raw)
		if err != nil {
			p.agentsErr = err
			reasons = append(reasons, "claude agents --json output unreadable: "+err.Error())
		}
	}
	if after := stampOf(lockPath); !before.exists && after.exists {
		reasons = append(reasons, "claude agents --json created "+lockPath+" (a daemon was started)")
	}

	// Fallback and merge source: the sessions registry.
	regs, bad, regErr := readRegistry(p.Paths.SessionsDir)
	if regErr != nil {
		reasons = append(reasons, "registry unreadable: "+regErr.Error())
	}
	if len(bad) > 0 {
		reasons = append(reasons, fmt.Sprintf("%d registry file(s) unreadable: %s", len(bad), strings.Join(bad, "; ")))
	}
	if p.agentsErr != nil && !errors.Is(p.agentsErr, errAgentsSkipped) && len(regs) == 0 && regErr != nil {
		reasons = append(reasons, "no liveness source available: "+p.agentsErr.Error())
	}

	// Merge by pid.
	byPID := map[int]*candidate{}
	var order []int
	for i := range regs {
		e := &regs[i]
		c := &candidate{pid: e.PID, reg: e, source: SourceRegistry}
		byPID[e.PID] = c
		order = append(order, e.PID)
	}
	for i := range agents {
		a := &agents[i]
		if a.PID <= 0 || a.SessionID == "" {
			continue
		}
		if c, ok := byPID[a.PID]; ok {
			c.agent = a
			c.source = SourceAgentsJSON
			continue
		}
		byPID[a.PID] = &candidate{pid: a.PID, agent: a, source: SourceAgentsJSON}
		order = append(order, a.PID)
	}
	sort.Ints(order)

	tbl, tblErr := snapshotProcs()
	if tblErr != nil {
		tbl = procTable{}
	}

	result := map[string]*model.Live{}
	for _, pid := range order {
		c := byPID[pid]
		lv := p.verify(c, tbl)
		if lv.SessionID == "" {
			continue
		}
		if prev, ok := result[lv.SessionID]; ok && !preferLive(lv, prev) {
			continue
		}
		result[lv.SessionID] = lv
	}

	if len(reasons) > 0 {
		p.degraded = true
		p.reason = strings.Join(reasons, "; ")
	}
	return result, nil
}

// preferLive decides whether a new Live for the same session id replaces
// the one already found: alive wins, then the later start.
func preferLive(candidate, existing *model.Live) bool {
	if candidate.Alive != existing.Alive {
		return candidate.Alive
	}
	return candidate.StartedAt.After(existing.StartedAt)
}

// verify checks that the recorded process is still the same claude process
// and fills a model.Live from the merged candidate.
func (p *Prober) verify(c *candidate, tbl procTable) *model.Live {
	lv := &model.Live{PID: c.pid, Source: c.source}
	if c.reg != nil {
		lv.SessionID = c.reg.SessionID
		lv.ProcStart = c.reg.ProcStart
		lv.StartedAt = msToTime(c.reg.StartedAt)
		lv.Name = c.reg.Name
		lv.NameSource = c.reg.NameSource
		lv.Status = c.reg.Status
		lv.Kind = c.reg.Kind
		lv.BridgeSessionID = c.reg.BridgeSessionID
	}
	if c.agent != nil {
		if c.agent.SessionID != "" {
			lv.SessionID = c.agent.SessionID
		}
		if c.agent.Name != "" {
			lv.Name = c.agent.Name
		}
		if c.agent.Status != "" {
			lv.Status = c.agent.Status
		}
		if c.agent.Kind != "" {
			lv.Kind = c.agent.Kind
		}
		if lv.StartedAt.IsZero() {
			lv.StartedAt = msToTime(c.agent.StartedAt)
		}
	}
	if lv.Status == "waiting" {
		lv.WaitingFor = "input"
	}
	if isBGKind(lv.Kind) {
		lv.BG = &model.BGInfo{ShortID: model.ShortID(lv.SessionID), DaemonAlive: p.daemonAlive()}
	}

	if !processExists(c.pid) {
		return lv
	}
	psStart, err := ProcessStart(c.pid)
	if err != nil {
		return lv
	}
	if lv.ProcStart == "" {
		lv.ProcStart = psStart
	}

	startOK := false
	if c.reg != nil && c.reg.ProcStart != "" && c.reg.ProcStart == psStart {
		startOK = true
	} else if !lv.StartedAt.IsZero() {
		if t, perr := parseLstart(psStart); perr == nil {
			d := lv.StartedAt.Sub(t)
			if d < 0 {
				d = -d
			}
			startOK = d <= startedAtTolerance
		}
	}

	tty, argv, _ := ttyArgs(c.pid)
	lv.TTY = tty
	lv.Argv = argv
	app, ttyFromTable := hostFromTable(tbl, c.pid)
	lv.HostApp = app
	if lv.TTY == "" {
		lv.TTY = ttyFromTable
	}

	signal := false
	if c.reg != nil && c.reg.MessagingSocketPath != "" {
		if _, serr := os.Lstat(c.reg.MessagingSocketPath); serr == nil {
			signal = true
		}
	}
	if !signal && argvLooksLikeClaude(argv) {
		signal = true
	}

	lv.Alive = startOK && signal
	return lv
}

func isBGKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "bg", "background", "daemon":
		return true
	}
	return false
}

// daemonAlive reports whether the daemon recorded in ClaudeDir/daemon.lock
// is still running. The lock file is read only, never written.
func (p *Prober) daemonAlive() bool {
	data, err := os.ReadFile(filepath.Join(p.Paths.ClaudeDir, "daemon.lock"))
	if err != nil {
		return false
	}
	var lock struct {
		PID       int    `json:"pid"`
		ProcStart string `json:"procStart"`
	}
	if json.Unmarshal(data, &lock) != nil || lock.PID <= 0 {
		return false
	}
	if !processExists(lock.PID) {
		return false
	}
	if lock.ProcStart == "" {
		return true
	}
	start, err := ProcessStart(lock.PID)
	return err == nil && start == lock.ProcStart
}
