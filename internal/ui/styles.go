package ui

import (
	"os"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/charmbracelet/lipgloss"
)

// Color tokens. Kept as named constants so tests can assert on them.
const (
	colorRed    = lipgloss.Color("1")
	colorGreen  = lipgloss.Color("2")
	colorYellow = lipgloss.Color("3")
	colorBlue   = lipgloss.Color("4")
	colorDim    = lipgloss.Color("8")
	colorDimmer = lipgloss.Color("238")
	colorWhite  = lipgloss.Color("15")
)

// styles is the palette of the UI. When color is false every style is a bare
// lipgloss.NewStyle() so that NO_COLOR output carries no escape codes.
type styles struct {
	color bool

	title     lipgloss.Style
	dim       lipgloss.Style
	dimmer    lipgloss.Style
	bold      lipgloss.Style
	selected  lipgloss.Style
	marked    lipgloss.Style
	banner    lipgloss.Style
	status    lipgloss.Style
	keybar    lipgloss.Style
	badge     lipgloss.Style
	header    lipgloss.Style
	dialog    lipgloss.Style
	needsYou  lipgloss.Style
	running   lipgloss.Style
	closed    lipgloss.Style
	gone      lipgloss.Style
	pendingOp lipgloss.Style
}

// noColor reports whether the NO_COLOR convention is active.
func noColor() bool {
	return os.Getenv("NO_COLOR") != ""
}

// newStyles builds the palette. With color=false no colors are attached.
func newStyles(color bool) styles {
	s := styles{color: color}
	if !color {
		plain := lipgloss.NewStyle()
		s.title, s.dim, s.dimmer, s.bold = plain, plain, plain, plain
		s.selected = plain.Reverse(true)
		s.marked, s.banner, s.status, s.keybar, s.badge, s.header = plain, plain, plain, plain, plain, plain
		s.dialog = plain.Border(lipgloss.RoundedBorder()).Padding(1, 2)
		s.needsYou, s.running, s.closed, s.gone, s.pendingOp = plain, plain, plain, plain, plain
		return s
	}
	s.title = lipgloss.NewStyle().Bold(true)
	s.dim = lipgloss.NewStyle().Foreground(colorDim)
	s.dimmer = lipgloss.NewStyle().Foreground(colorDimmer)
	s.bold = lipgloss.NewStyle().Bold(true)
	s.selected = lipgloss.NewStyle().Reverse(true)
	s.marked = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	s.banner = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	s.status = lipgloss.NewStyle().Foreground(colorWhite)
	s.keybar = lipgloss.NewStyle().Foreground(colorDim)
	s.badge = lipgloss.NewStyle().Foreground(colorBlue)
	s.header = lipgloss.NewStyle().Bold(true)
	s.dialog = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBlue).Padding(1, 2)
	s.needsYou = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	s.running = lipgloss.NewStyle().Foreground(colorGreen)
	s.closed = lipgloss.NewStyle().Foreground(colorDim)
	s.gone = lipgloss.NewStyle().Foreground(colorDimmer)
	s.pendingOp = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	return s
}

// forState returns the style used for the dot and word of a state.
func (s styles) forState(st model.State) lipgloss.Style {
	switch st.Word() {
	case "Needs you":
		return s.needsYou
	case "Running":
		return s.running
	case "Gone":
		return s.gone
	default:
		return s.closed
	}
}

// stateDot returns the dot glyph for a state. The word is always painted next
// to it so the state is readable without color.
func stateDot(st model.State) string {
	switch st.Word() {
	case "Needs you":
		return "●"
	case "Running":
		return "●"
	case "Gone":
		return "○"
	default:
		return "·"
	}
}
