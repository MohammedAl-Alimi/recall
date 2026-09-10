package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// ReadSettings parses settings.json into a generic map. Numbers are kept as
// json.Number so a later WriteSettings reproduces them verbatim. A missing
// file yields an empty map.
func ReadSettings(p model.Paths) (map[string]any, error) {
	data, err := os.ReadFile(p.SettingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return parseSettings(data)
}

// WriteSettings replaces settings.json with obj. The previous content is
// kept as settings.json.bak and the new file is written with temp + rename
// preserving the previous mode (0600 for a new file).
func WriteSettings(p model.Paths, obj map[string]any) error {
	if p.SettingsFile == "" {
		return errors.New("archive: no settings file path")
	}
	if obj == nil {
		return errors.New("archive: nil settings")
	}
	mode := os.FileMode(0o600)
	orig, err := os.ReadFile(p.SettingsFile)
	switch {
	case err == nil:
		if st, err := os.Stat(p.SettingsFile); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.WriteFile(p.SettingsFile+".bak", orig, mode); err != nil {
			return fmt.Errorf("archive: write backup: %w", err)
		}
	case os.IsNotExist(err):
	default:
		return err
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return writeAtomic(p.SettingsFile, out, mode)
}
