package hook

import (
	"errors"
	"io"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Handle processes one Claude hook invocation.
func Handle(p model.Paths, event string, stdin io.Reader) error {
	return errors.New("not implemented: hook.Handle")
}

// InstallSettings merges recall hooks for events into settings.json.
func InstallSettings(p model.Paths, events []string, binPath string) error {
	return errors.New("not implemented: hook.InstallSettings")
}

// UninstallSettings removes recall hooks from settings.json.
func UninstallSettings(p model.Paths) error {
	return errors.New("not implemented: hook.UninstallSettings")
}
