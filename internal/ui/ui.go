package ui

import (
	"fmt"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Keep the TUI dependencies in go.mod until the screens are implemented.
var (
	_ = tea.NewProgram
	_ = key.NewBinding
	_ = lipgloss.NewStyle
)

// RunOptions configures the TUI start.
type RunOptions struct {
	FromWidget   bool
	InitialQuery string
}

// Run starts the Bubble Tea program.
func Run(a *app.App, opts RunOptions) error {
	fmt.Println("ui not implemented")
	return nil
}
