package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Retention reads cleanupPeriodDays from settings.json. A missing file or
// a missing key returns set=false with a nil error.
func Retention(p model.Paths) (days int, set bool, err error) {
	data, err := os.ReadFile(p.SettingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	obj, err := parseSettings(data)
	if err != nil {
		return 0, false, err
	}
	raw, ok := obj[RetentionKey]
	if !ok || raw == nil {
		return 0, false, nil
	}
	d, err := toDays(raw)
	if err != nil {
		return 0, false, fmt.Errorf("archive: settings %s: %w", RetentionKey, err)
	}
	return d, true, nil
}

// SetRetention writes cleanupPeriodDays into settings.json. Every other key
// is preserved. The previous file is kept as settings.json.bak, the new file
// is written with temp + rename and re-read to verify the value. The whole
// sequence is retried up to three times.
func SetRetention(p model.Paths, days int) error {
	if days <= 0 {
		return errors.New("archive: retention days must be positive")
	}
	if p.SettingsFile == "" {
		return errors.New("archive: no settings file path")
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
		}
		if err := setRetentionOnce(p, days); err != nil {
			last = err
			continue
		}
		got, set, err := Retention(p)
		if err != nil {
			last = err
			continue
		}
		if !set || got != days {
			last = fmt.Errorf("archive: verify failed: %s=%d set=%v", RetentionKey, got, set)
			continue
		}
		return nil
	}
	return last
}

func setRetentionOnce(p model.Paths, days int) error {
	mode := os.FileMode(0o600)
	var orig []byte
	data, err := os.ReadFile(p.SettingsFile)
	switch {
	case err == nil:
		orig = data
		if st, err := os.Stat(p.SettingsFile); err == nil {
			mode = st.Mode().Perm()
		}
	case os.IsNotExist(err):
		orig = nil
	default:
		return err
	}
	obj, err := parseSettings(orig)
	if err != nil {
		return err
	}
	obj[RetentionKey] = json.Number(strconv.Itoa(days))
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if orig != nil {
		if err := os.WriteFile(p.SettingsFile+".bak", orig, mode); err != nil {
			return fmt.Errorf("archive: write backup: %w", err)
		}
	}
	return writeAtomic(p.SettingsFile, out, mode)
}

// parseSettings decodes settings.json into a generic map, keeping numbers as
// json.Number so they are written back unchanged. Empty input yields an
// empty map.
func parseSettings(data []byte) (map[string]any, error) {
	obj := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return obj, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("archive: settings.json is not a JSON object: %w", err)
	}
	return obj, nil
}

func toDays(v any) (int, error) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, err
		}
		return floatDays(f)
	case float64:
		return floatDays(n)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("not a number: %q", n)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("unexpected type %T", v)
	}
}

func floatDays(f float64) (int, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, fmt.Errorf("not an integer: %v", f)
	}
	return int(f), nil
}
