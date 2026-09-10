package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

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

// writeLsTable prints a plain aligned table.
func writeLsTable(w io.Writer, list []*model.Session, now time.Time) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATE\tAGE\tPROJECT\tBRANCH\tTITLE")
	for _, s := range list {
		title := s.Title
		if s.Label != "" {
			title = s.Label + ": " + title
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Short(), s.State.Word(), humanAge(now, s.LastActive), truncate(projectName(s), 28), truncate(s.Branch, 24), truncate(title, 60))
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
			writeLsTable(cmd.OutOrStdout(), list, time.Now())
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
