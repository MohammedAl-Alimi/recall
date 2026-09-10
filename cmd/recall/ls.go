package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// lsLink is the JSON shape of a PR link in 'recall ls --json'.
type lsLink struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// lsLive is the JSON shape of the live block in 'recall ls --json'.
type lsLive struct {
	PID     int    `json:"pid"`
	Status  string `json:"status"`
	HostApp string `json:"hostApp"`
	TTY     string `json:"tty"`
}

// lsRow is the stable JSON record printed by 'recall ls --json'. Fields are
// never omitted so consumers can rely on the key set; absent values are
// null, "" or 0.
type lsRow struct {
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
	PRs           []lsLink  `json:"prs"`
	Live          *lsLive   `json:"live"`
	Interrupted   bool      `json:"interrupted"`
	Headless      bool      `json:"headless"`
	Ghost         bool      `json:"ghost"`
	Archived      bool      `json:"archived"`
	// ParseErrors counts transcript records the scanner did not recognise.
	// A rising count across sessions is the documented signal that the
	// transcript format changed (docs/compatibility.md).
	ParseErrors int `json:"parseErrors"`
}

// projectName is the user-facing project name of a session: the basename of
// its working directory.
func projectName(s *model.Session) string {
	dir := s.WorkCwd
	if dir == "" {
		dir = s.LastCwd
	}
	if dir == "" {
		dir = s.Cwd
	}
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

func toLsRow(s *model.Session) lsRow {
	row := lsRow{
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
		PRs:           []lsLink{},
		Interrupted:   s.Interrupted,
		Headless:      s.Headless,
		Ghost:         s.Ghost,
		Archived:      s.Archived,
		ParseErrors:   s.ParseErrors,
	}
	for _, l := range s.PRs {
		row.PRs = append(row.PRs, lsLink{URL: l.URL, Number: l.Number, Title: l.Title})
	}
	if s.Live != nil {
		row.Live = &lsLive{PID: s.Live.PID, Status: s.Live.Status, HostApp: s.Live.HostApp, TTY: s.Live.TTY}
	}
	return row
}

// writeLsJSON prints list as a JSON array (never null).
func writeLsJSON(w io.Writer, list []*model.Session) error {
	rows := make([]lsRow, 0, len(list))
	for _, s := range list {
		rows = append(rows, toLsRow(s))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

// Column limits for the ls table. The full layout is used when stdout is
// not a terminal; on a terminal the title takes whatever is left of the
// width and the branch column is dropped when the terminal is narrow.
const (
	lsProjectMax   = 28
	lsBranchMax    = 24
	lsTitleMax     = 60
	lsTitleMin     = 24
	lsBranchMinCol = 100
	lsDefaultWidth = 120
	lsColumnGap    = 2
)

// lsLayout is the column plan for one ls table.
type lsLayout struct {
	project, branch, title int
	showBranch             bool
}

// lsLayoutFor sizes the columns for width. A width of 0 means unlimited
// (the classic layout). Otherwise the fixed columns are measured against
// the rows, the branch column is dropped below lsBranchMinCol, and the
// title receives the remainder so no row wraps.
func lsLayoutFor(width int, list []*model.Session, now time.Time) lsLayout {
	if width <= 0 {
		return lsLayout{project: lsProjectMax, branch: lsBranchMax, title: lsTitleMax, showBranch: true}
	}
	idW, stateW, ageW, projectW, branchW := len("ID"), len("STATE"), len("AGE"), len("PROJECT"), len("BRANCH")
	for _, s := range list {
		idW = max(idW, len([]rune(s.Short())))
		stateW = max(stateW, len(s.State.Word()))
		ageW = max(ageW, len(humanAge(now, s.LastActive)))
		projectW = max(projectW, min(lsProjectMax, len([]rune(projectName(s)))))
		branchW = max(branchW, min(lsBranchMax, len([]rune(s.Branch))))
	}
	l := lsLayout{project: projectW, branch: branchW, showBranch: width >= lsBranchMinCol}
	base := idW + stateW + ageW + 4*lsColumnGap
	remaining := func() int {
		n := width - base - l.project
		if l.showBranch {
			n -= l.branch + lsColumnGap
		}
		return n
	}
	// Give room back in order: shrink the branch, drop it, shrink the
	// project, and only then let the title fall below its minimum.
	if remaining() < lsTitleMin && l.showBranch {
		l.branch = max(len("BRANCH"), l.branch-(lsTitleMin-remaining()))
	}
	if remaining() < lsTitleMin && l.showBranch {
		l.showBranch = false
	}
	if remaining() < lsTitleMin {
		l.project = max(len("PROJECT"), l.project-(lsTitleMin-remaining()))
	}
	l.title = max(lsTitleMin, remaining())
	return l
}

// termWidth returns the column count of w when it is a terminal, else 0
// (print everything). Falls back to $COLUMNS, then lsDefaultWidth, when the
// size query fails.
func termWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		// ENOTTY: a pipe or a file, print the full layout.
		return 0
	}
	if ws.Col > 0 {
		return int(ws.Col)
	}
	if n := widthFromEnv(os.Getenv("COLUMNS")); n > 0 {
		return n
	}
	return lsDefaultWidth
}

// widthFromEnv parses a $COLUMNS value; anything unusable is 0.
func widthFromEnv(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// writeLsTable prints a plain aligned table sized for width columns
// (0: unlimited).
func writeLsTable(w io.Writer, list []*model.Session, now time.Time, width int) {
	l := lsLayoutFor(width, list, now)
	tw := tabwriter.NewWriter(w, 0, 0, lsColumnGap, ' ', 0)
	if l.showBranch {
		fmt.Fprintln(tw, "ID\tSTATE\tAGE\tPROJECT\tBRANCH\tTITLE")
	} else {
		fmt.Fprintln(tw, "ID\tSTATE\tAGE\tPROJECT\tTITLE")
	}
	for _, s := range list {
		title := s.Title
		if s.Label != "" {
			title = s.Label + ": " + title
		}
		cells := []string{s.Short(), s.State.Word(), humanAge(now, s.LastActive), truncate(projectName(s), l.project)}
		if l.showBranch {
			cells = append(cells, truncate(s.Branch, l.branch))
		}
		cells = append(cells, truncate(title, l.title))
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()
}

// humanAge formats the distance between now and t as a short word.
func humanAge(now, t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", "")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// filterProject keeps sessions whose working directory contains sub.
func filterProject(list []*model.Session, sub string) []*model.Session {
	if sub == "" {
		return list
	}
	var out []*model.Session
	for _, s := range list {
		if strings.Contains(s.WorkCwd, sub) || strings.Contains(s.Cwd, sub) || strings.Contains(s.LastCwd, sub) || projectName(s) == sub {
			out = append(out, s)
		}
	}
	return out
}

// filterLive keeps sessions with a running claude process.
func filterLive(list []*model.Session) []*model.Session {
	var out []*model.Session
	for _, s := range list {
		if s.State.IsLive() {
			out = append(out, s)
		}
	}
	return out
}

func newLsCmd() *cobra.Command {
	var (
		asJSON, all, onlyLive, ghosts, headless bool
		project                                 string
	)
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.Load(context.Background(), ghosts); err != nil {
				return err
			}
			list := a.Visible(all, ghosts, headless)
			list = filterProject(list, project)
			if onlyLive {
				list = filterLive(list)
			}
			if asJSON {
				return writeLsJSON(cmd.OutOrStdout(), list)
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
				return nil
			}
			writeLsTable(cmd.OutOrStdout(), list, time.Now(), termWidth(cmd.OutOrStdout()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print a JSON array with a stable field set")
	cmd.Flags().BoolVar(&all, "all", false, "include hidden sessions")
	cmd.Flags().BoolVar(&onlyLive, "live", false, "only sessions with a running claude process")
	cmd.Flags().BoolVar(&ghosts, "ghosts", false, "include ghost sessions (history only, transcript gone)")
	cmd.Flags().BoolVar(&headless, "headless", false, "include headless (sdk, -p) sessions")
	cmd.Flags().StringVar(&project, "project", "", "filter by project path substring or basename")
	return cmd
}
