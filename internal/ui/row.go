package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/charmbracelet/lipgloss"
)

// narrowWidth is the terminal width below which columns collapse.
const narrowWidth = 80

// rowOpts controls how one list row is painted.
type rowOpts struct {
	width    int
	selected bool
	// marked is true when the row is checked in multi-select mode.
	marked bool
	// selectMode paints a checkbox column even for unmarked rows.
	selectMode bool
	// pending is the key of a destructive action waiting for its second
	// press ("x" or "D"), or empty.
	pending string
	now     time.Time
}

// renderRow paints a two-line row for sess. The first line carries the state
// dot and word, the title, project, branch and last-active age; the second
// dim line carries the last prompt or assistant text and badges.
func renderRow(st styles, sess *model.Session, o rowOpts) string {
	width := o.width
	if width < 20 {
		width = 20
	}
	line1 := renderRowLine1(st, sess, o, width)
	line2 := renderRowLine2(st, sess, o, width)
	return line1 + "\n" + line2
}

func rowPrefix(st styles, o rowOpts) string {
	cursor := "  "
	if o.selected {
		cursor = "> "
	}
	if o.pending != "" {
		cursor = st.pendingOp.Render("!!")
		if !o.selected {
			cursor = st.pendingOp.Render("! ")
		}
	}
	if o.selectMode {
		box := "[ ] "
		if o.marked {
			box = st.marked.Render("[x] ")
		}
		return cursor + box
	}
	return cursor
}

func renderRowLine1(st styles, sess *model.Session, o rowOpts, width int) string {
	prefix := rowPrefix(st, o)
	prefixW := lipgloss.Width(prefix)

	stateCell := st.forState(sess.State).Render(stateDot(sess.State) + " " + padRight(sess.State.Word(), 9))
	stateW := lipgloss.Width(stateCell)

	age := humanAge(sess.LastActive, o.now)
	ageCell := st.dim.Render(padLeft(age, 8))
	ageW := lipgloss.Width(ageCell)

	narrow := width < narrowWidth
	project := projectName(sess)
	branch := sess.Branch
	if sess.IsWorktree && sess.WorktreeBranch != "" {
		branch = sess.WorktreeBranch
	}

	// Right side columns: project and branch on wide screens only.
	right := ""
	if !narrow {
		parts := []string{}
		if project != "" {
			parts = append(parts, st.dim.Render(truncate(project, 22)))
		}
		if branch != "" {
			parts = append(parts, st.dim.Render(truncate(branch, 22)))
		}
		right = strings.Join(parts, "  ")
	}
	rightW := lipgloss.Width(right)
	if rightW > 0 {
		rightW += 2
	}

	titleW := width - prefixW - stateW - 2 - rightW - 2 - ageW
	if titleW < 8 {
		titleW = 8
	}
	title := rowTitle(st, sess, titleW)

	line := prefix + stateCell + "  " + padRight(title, titleW)
	if right != "" {
		line += "  " + right
	}
	line += "  " + ageCell
	if o.selected && st.color {
		return st.selected.Render(fit(line, width))
	}
	if o.selected {
		return fit(line, width)
	}
	return fit(line, width)
}

func renderRowLine2(st styles, sess *model.Session, o rowOpts, width int) string {
	indent := "  "
	if o.selectMode {
		indent = "      "
	}
	narrow := width < narrowWidth

	badges := rowBadges(st, sess)
	badgesW := lipgloss.Width(badges)
	if badgesW > 0 {
		badgesW += 2
	}

	var text string
	if narrow {
		parts := []string{}
		if p := projectName(sess); p != "" {
			parts = append(parts, p)
		}
		if sess.Branch != "" {
			parts = append(parts, sess.Branch)
		}
		text = strings.Join(parts, "  ")
		if text == "" {
			text = rowSummaryText(sess)
		}
	} else {
		text = rowSummaryText(sess)
	}
	// Two extra spaces at the front so the text sits under the title column.
	textW := width - len(indent) - 12 - badgesW
	if textW < 8 {
		textW = 8
	}
	line := indent + strings.Repeat(" ", 12) + st.dim.Render(padRight(truncate(text, textW), textW))
	if badges != "" {
		line += "  " + badges
	}
	return fit(line, width)
}

// rowTitle returns the title cell: label (bold) then title, or the title
// alone. Pinned sessions get a star.
func rowTitle(st styles, sess *model.Session, w int) string {
	title := oneLine(sess.Title)
	if title == "" {
		title = oneLine(sess.FirstPrompt)
	}
	if title == "" {
		title = "(untitled) " + sess.Short()
	}
	pin := ""
	if sess.Pinned {
		pin = "★ "
	}
	if sess.Label != "" {
		label := oneLine(sess.Label)
		rest := w - len([]rune(pin)) - len([]rune(label)) - 2
		if rest < 6 {
			return pin + st.title.Render(truncate(label, w-len([]rune(pin))))
		}
		return pin + st.title.Render(label) + "  " + st.dim.Render(truncate(title, rest))
	}
	return pin + st.title.Render(truncate(title, w-len([]rune(pin))))
}

// rowSummaryText prefers the last prompt, then the last assistant text, then
// the first prompt, then ghost prompts.
func rowSummaryText(sess *model.Session) string {
	for _, c := range []string{sess.LastAssistant, sess.LastPrompt, sess.FirstPrompt} {
		if t := oneLine(c); t != "" {
			return t
		}
	}
	if len(sess.GhostPrompts) > 0 {
		return oneLine(sess.GhostPrompts[len(sess.GhostPrompts)-1])
	}
	if sess.Ghost {
		return "no transcript on disk"
	}
	return ""
}

// rowBadges returns the badge cell: PR#N, wt, +N dirs, fork, and notes.
func rowBadges(st styles, sess *model.Session) string {
	var b []string
	for i, pr := range sess.PRs {
		if i >= 2 {
			break
		}
		if pr.Number > 0 {
			b = append(b, fmt.Sprintf("PR#%d", pr.Number))
		}
	}
	if sess.IsWorktree {
		b = append(b, "wt")
	}
	if n := len(sess.Cwds); n > 1 {
		b = append(b, fmt.Sprintf("+%d dirs", n-1))
	}
	if sess.Lineage != "" && sess.Lineage != "root" {
		b = append(b, "fork")
	}
	if sess.Interrupted {
		b = append(b, "interrupted")
	}
	if sess.Archived {
		b = append(b, "archived")
	}
	for _, t := range sess.Tags {
		b = append(b, "#"+t)
	}
	if len(b) == 0 {
		return ""
	}
	return st.badge.Render(strings.Join(b, " "))
}

// projectName returns the basename of the session's working directory.
func projectName(sess *model.Session) string {
	for _, c := range []string{sess.WorkCwd, sess.LastCwd, sess.Cwd} {
		if c != "" {
			return filepath.Base(c)
		}
	}
	return ""
}

// humanAge formats t relative to now: "now", "3m", "2h", "yesterday", "5d",
// or a date.
func humanAge(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("Jan 02")
	}
}

// oneLine collapses whitespace so text fits on a single row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate shortens s to at most n cells, appending an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func padLeft(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return strings.Repeat(" ", n-w) + s
}

// fit cuts a styled line at width cells without breaking escape sequences.
func fit(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
