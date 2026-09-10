package app

import (
	"context"
	"errors"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/live"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/mux"
	"github.com/MohammedAl-Alimi/recall/internal/scan"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// App is the shared application state.
type App struct {
	Paths   model.Paths
	Scanner *scan.Scanner
	Prober  *live.Prober
	Tmux    *mux.Tmux
	Meta    *state.Meta

	Sessions []*model.Session
	LiveByID map[string]*model.Live

	Degraded       bool
	DegradedReason string
	RetentionDays  int
	RetentionSet   bool
	ShellCwd       string
}

// New builds an App for p without loading anything.
func New(p model.Paths) (*App, error) {
	return &App{
		Paths:    p,
		Scanner:  scan.New(p),
		Prober:   live.New(p),
		Tmux:     mux.NewTmux(),
		Meta:     state.NewMeta(),
		LiveByID: map[string]*model.Live{},
	}, nil
}

// Load scans transcripts, ghosts, metadata and live processes, then derives
// every session state.
func (a *App) Load(ctx context.Context, includeGhosts bool) error {
	return errors.New("not implemented: app.App.Load")
}

// RefreshLive re-probes live processes and re-derives states.
func (a *App) RefreshLive(ctx context.Context) error {
	return errors.New("not implemented: app.App.RefreshLive")
}

// Visible returns the sessions to display given the toggles.
func (a *App) Visible(showHidden, showGhosts, showHeadless bool) []*model.Session {
	return nil
}

// Open locks, plans and records the launch of sess.
func (a *App) Open(sess *model.Session, opts launch.Options) (*launch.Action, error) {
	return nil, errors.New("not implemented: app.App.Open")
}

// PreselectForCwd returns the single session active in the last 7 days whose
// working directory equals ShellCwd, or nil.
func (a *App) PreselectForCwd() *model.Session {
	return nil
}

// NotifyNeedsYou sends a notification for every session that transitioned
// into needs_you since prev.
func (a *App) NotifyNeedsYou(prev map[string]model.State) {}
