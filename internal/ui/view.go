package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/charmbracelet/lipgloss"
)

// keyBar is painted on the last line of every screen.
const keyBar = "Enter open · Space preview · / search · n new · f fork · ? keys"

// View paints the whole screen.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	header := m.viewHeader()
	body := m.viewBody()
	footer := m.viewFooter()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// headerLines returns the header text lines (title line, banners, spacer).
func (m *Model) headerLines() []string {
	st := m.st
	summary := m.summary
	if summary == "" {
		if m.loaded {
			summary = fmt.Sprintf("%d sessions", len(m.rows))
		} else {
			summary = "scanning"
		}
	}
	left := st.header.Render("recall") + "  " + summary
	flags := []string{"scope: " + string(m.scope)}
	if m.query != "" {
		flags = append(flags, "search: "+m.query)
	}
	if m.showHidden {
		flags = append(flags, "hidden")
	}
	if m.showGhosts {
		flags = append(flags, "ghosts")
	}
	if m.selectMode {
		flags = append(flags, fmt.Sprintf("select: %d marked", len(m.marked)))
	}
	right := st.dim.Render(strings.Join(flags, " · "))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 2 {
		gap = 2
	}
	lines := []string{fit(" "+left+strings.Repeat(" ", gap)+right, m.width)}

	for _, b := range m.banners() {
		lines = append(lines, fit(" "+st.banner.Render("! "+b), m.width))
	}
	lines = append(lines, "")
	return lines
}

// banners lists the yellow warnings for the header.
func (m *Model) banners() []string {
	var out []string
	if m.app != nil {
		if m.loaded && !m.app.RetentionSet {
			out = append(out, "Retention is not set: Claude deletes transcripts after 30 days. Press S for setup.")
		}
		if m.app.Degraded {
			reason := m.app.DegradedReason
			if reason == "" {
				reason = "live process detection is unreliable"
			}
			out = append(out, "Liveness degraded: "+reason)
		}
	}
	return out
}

func (m *Model) viewHeader() string {
	return strings.Join(m.headerLines(), "\n")
}

// listHeight is the number of lines available for rows or the preview.
func (m *Model) listHeight() int {
	h := m.height - len(m.headerLines()) - 2
	if h < rowHeight {
		h = rowHeight
	}
	return h
}

func (m *Model) viewBody() string {
	h := m.listHeight()
	switch m.mode {
	case modeHelp:
		return m.overlay(m.viewHelp(), h)
	case modePalette:
		return m.overlay(m.viewPalette(), h)
	case modeCwd:
		return m.overlay(m.viewCwdDialog(), h)
	case modeLabel, modeTag:
		return m.overlay(m.viewInputDialog(), h)
	}
	if m.preview {
		if m.width >= sideBySideWidth {
			lw := m.width * 55 / 100
			pw := m.width - lw - 3
			list := m.viewList(lw, h)
			prev := m.viewPreview(pw, h)
			border := lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(colorDim).PaddingLeft(1)
			if !m.st.color {
				border = lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).PaddingLeft(1)
			}
			return lipgloss.JoinHorizontal(lipgloss.Top, list, border.Render(prev))
		}
		return m.viewPreview(m.width, h)
	}
	return m.viewList(m.width, h)
}

// viewList paints the visible window of rows.
func (m *Model) viewList(width, height int) string {
	if len(m.rows) == 0 {
		msg := "no sessions"
		if !m.loaded {
			msg = "scanning transcripts"
		} else if m.query != "" {
			msg = "no sessions match " + m.query
		}
		return lipgloss.NewStyle().Width(width).Height(height).Render("  " + m.st.dim.Render(msg))
	}
	page := height / rowHeight
	if page < 1 {
		page = 1
	}
	if m.offset > m.cursor {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	end := min(len(m.rows), m.offset+page)
	now := m.now()
	var b strings.Builder
	for i := m.offset; i < end; i++ {
		s := m.rows[i]
		o := rowOpts{
			width:      width,
			selected:   i == m.cursor,
			marked:     m.marked[s.ID],
			selectMode: m.selectMode,
			now:        now,
		}
		if m.pending != nil && m.pending.sid == s.ID {
			o.pending = infoFor(m.pending.act).key
		}
		b.WriteString(renderRow(m.st, s, o))
		b.WriteString("\n")
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	if end < len(m.rows) {
		b.WriteString(m.st.dim.Render(fmt.Sprintf("  %d more below", len(m.rows)-end)))
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(b.String())
}

// viewPreview paints the preview pane for the selected session.
func (m *Model) viewPreview(width, height int) string {
	sess := m.selected()
	if sess == nil {
		return lipgloss.NewStyle().Width(width).Height(height).Render(m.st.dim.Render("nothing selected"))
	}
	lines := previewLines(m.st, sess, width, m.now())
	if m.previewScroll > len(lines)-1 {
		m.previewScroll = max(0, len(lines)-1)
	}
	lines = lines[m.previewScroll:]
	if len(lines) > height {
		lines = lines[:height]
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(strings.Join(lines, "\n"))
}

// previewLines renders the preview text for sess, wrapped at width.
func previewLines(st styles, sess *model.Session, width int, now time.Time) []string {
	wrap := lipgloss.NewStyle().Width(max(20, width))
	var out []string
	add := func(s string) { out = append(out, strings.Split(wrap.Render(s), "\n")...) }
	field := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		add(st.dim.Render(padRight(k, 12)) + v)
	}

	title := oneLine(sess.Title)
	if sess.Label != "" {
		title = sess.Label + "  " + st.dim.Render(title)
	}
	add(st.forState(sess.State).Render(stateDot(sess.State)+" "+sess.State.Word()) + "  " + st.title.Render(title))
	if hint := sess.State.Hint(); hint != "" {
		add(st.forState(sess.State).Render(hint))
	}
	add("")
	field("id", sess.ID)
	field("state", string(sess.State))
	field("project", projectName(sess))
	field("dir", displayPath(sessionDir(sess)))
	if len(sess.Cwds) > 1 {
		field("dirs", fmt.Sprintf("%d directories", len(sess.Cwds)))
	}
	branch := sess.Branch
	if sess.IsWorktree {
		branch += " (worktree)"
	}
	field("branch", branch)
	if !sess.LastActive.IsZero() {
		field("active", humanAge(sess.LastActive, now)+"  "+sess.LastActive.Local().Format("2006-01-02 15:04"))
	}
	if !sess.CreatedAt.IsZero() {
		field("created", sess.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	if sess.Turns > 0 {
		field("turns", fmt.Sprintf("%d", sess.Turns))
	}
	if sess.Compactions > 0 {
		field("compacted", fmt.Sprintf("%d times", sess.Compactions))
	}
	if sess.ContextTokens > 0 {
		field("context", fmt.Sprintf("%dk tokens", sess.ContextTokens/1000))
	}
	if sess.CostUSD > 0 {
		field("cost", fmt.Sprintf("$%.2f", sess.CostUSD))
	}
	field("mode", sess.PermMode)
	field("entry", sess.Entrypoint)
	field("lineage", sess.Lineage)
	if sess.Interrupted {
		field("note", "interrupted")
	}
	if sess.DanglingTool != "" {
		field("dangling", sess.DanglingTool)
	}
	if sess.BgJobsLost > 0 {
		field("bg lost", fmt.Sprintf("%d background jobs", sess.BgJobsLost))
	}
	if sess.Live != nil {
		l := sess.Live
		live := fmt.Sprintf("pid %d", l.PID)
		if l.HostApp != "" {
			live += "  " + l.HostApp
		}
		if l.TTY != "" {
			live += "  " + l.TTY
		}
		if l.Status != "" {
			live += "  " + l.Status
		}
		if l.WaitingFor != "" {
			live += "  waiting: " + l.WaitingFor
		}
		field("live", live)
		if l.Mux != nil {
			field("tmux", l.Mux.SessionName)
		}
	}
	for _, pr := range sess.PRs {
		field("PR", fmt.Sprintf("#%d %s", pr.Number, pr.URL))
	}
	for _, a := range sess.Artifacts {
		field("artifact", strings.TrimSpace(a.Title+" "+a.URL))
	}
	if sess.BridgeURL != "" {
		field("claude.ai", sess.BridgeURL)
	}
	if len(sess.Tags) > 0 {
		field("tags", strings.Join(sess.Tags, " "))
	}
	field("notes", sess.Notes)
	if len(sess.FilesChanged) > 0 {
		add("")
		add(st.dim.Render("files changed"))
		for i, f := range sess.FilesChanged {
			if i >= 10 {
				add(st.dim.Render(fmt.Sprintf("  and %d more", len(sess.FilesChanged)-10)))
				break
			}
			add("  " + f)
		}
	}
	if sess.Launch != nil && len(sess.Launch.Argv) > 0 {
		add("")
		add(st.dim.Render("launched as"))
		add("  " + strings.Join(sess.Launch.Argv, " "))
	}
	section := func(name, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		add("")
		add(st.dim.Render(name))
		add(truncate(oneLine(text), 600))
	}
	section("first prompt", sess.FirstPrompt)
	section("last prompt", sess.LastPrompt)
	section("last reply", sess.LastAssistant)
	if sess.Ghost && len(sess.GhostPrompts) > 0 {
		add("")
		add(st.dim.Render("prompts from history (no transcript)"))
		for i, p := range sess.GhostPrompts {
			if i >= 8 {
				break
			}
			add("  " + truncate(oneLine(p), 200))
		}
	}
	if sess.Archived {
		add("")
		field("archive", sess.ArchivePath)
	}
	return out
}

// overlay centers content in the body area.
func (m *Model) overlay(content string, height int) string {
	box := m.st.dialog.MaxWidth(m.width - 2).Render(content)
	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) viewHelp() string {
	var b strings.Builder
	b.WriteString(m.st.title.Render("keys"))
	b.WriteString("\n\n")
	for _, i := range actionTable {
		key := i.key
		if key == "" {
			key = "(palette)"
		}
		line := padRight(key, 10) + i.label
		if i.disabled {
			line = m.st.dim.Render(line + " (disabled)")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.st.dim.Render("Space marks rows in select mode (V). Press any key to close."))
	return b.String()
}

func (m *Model) viewPalette() string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	items := paletteItems(m.input.Value())
	if len(items) == 0 {
		b.WriteString(m.st.dim.Render("no matching command"))
	}
	for i, it := range items {
		if i >= 14 {
			b.WriteString(m.st.dim.Render(fmt.Sprintf("  %d more", len(items)-i)))
			break
		}
		cursor := "  "
		if i == m.paletteIdx {
			cursor = "> "
		}
		line := cursor + padRight(it.key, 8) + it.label
		if it.disabled {
			line = m.st.dim.Render(line)
		} else if i == m.paletteIdx {
			line = m.st.bold.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewCwdDialog() string {
	d := m.dialog
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.st.title.Render("directory missing"))
	b.WriteString("\n\n")
	b.WriteString(displayPath(d.missing))
	b.WriteString("\n\n")
	if d.original != "" {
		b.WriteString("o  open in the original directory " + displayPath(d.original) + "\n")
	} else {
		b.WriteString(m.st.dim.Render("o  original directory also missing") + "\n")
	}
	b.WriteString("h  open in your home directory\n")
	b.WriteString("c  cancel")
	return b.String()
}

func (m *Model) viewInputDialog() string {
	what := "label"
	if m.mode == modeTag {
		what = "tags"
	}
	sess := m.selected()
	title := ""
	if sess != nil {
		title = truncate(oneLine(sess.Title), 50)
	}
	return m.st.title.Render(what) + "  " + m.st.dim.Render(title) + "\n\n" + m.input.View() + "\n\n" + m.st.dim.Render("Enter saves, Esc cancels")
}

// viewFooter paints the status line and the key bar.
func (m *Model) viewFooter() string {
	status := m.status
	if m.mode == modeSearch {
		status = m.input.View()
	} else if status == "" {
		if m.busy && !m.loaded {
			status = m.st.dim.Render("scanning")
		} else if s := m.selected(); s != nil {
			status = m.st.dim.Render(s.Short() + "  " + displayPath(sessionDir(s)))
		}
	} else {
		status = m.st.status.Render(status)
	}
	bar := keyBar
	if m.selectMode {
		bar = "Space mark · H hide · D delete · V done · " + keyBar
	}
	return fit(" "+status, m.width) + "\n" + fit(" "+m.st.keybar.Render(bar), m.width)
}
