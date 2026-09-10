package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// TestLsJSONExposesParseErrors checks the detection signal promised by
// docs/compatibility.md: the unknown-record count of each transcript is
// printed as parseErrors, and is 0 (never absent) for a clean one.
func TestLsJSONExposesParseErrors(t *testing.T) {
	list := []*model.Session{
		{ID: "aaaaaaaa-0000-4000-8000-000000000000", ParseErrors: 3},
		{ID: "bbbbbbbb-0000-4000-8000-000000000000"},
	}
	var buf bytes.Buffer
	if err := writeLsJSON(&buf, list); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if got := rows[0]["parseErrors"]; got != float64(3) {
		t.Errorf("parseErrors = %v, want 3", got)
	}
	if got, ok := rows[1]["parseErrors"]; !ok || got != float64(0) {
		t.Errorf("clean row parseErrors = %v (present %v), want 0", got, ok)
	}
}
