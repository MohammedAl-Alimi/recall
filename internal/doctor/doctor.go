package doctor

import (
	"errors"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// MinClaudeVersion is the lowest claude CLI version recall supports.
const MinClaudeVersion = "2.1.223"

// Check is one doctor result.
type Check struct {
	Name string
	// Status is one of "ok", "warn", "fail".
	Status string
	Detail string
}

// Features lists the claude CLI capabilities detected from --help.
type Features struct {
	Version     string
	Resume      bool
	ForkSession bool
	SessionID   bool
	Name        bool
	BG          bool
	AgentsJSON  bool
	Attach      bool
}

// DetectFeatures parses 'claude --version' and 'claude --help'.
func DetectFeatures() (Features, error) {
	return Features{}, errors.New("not implemented: doctor.DetectFeatures")
}

// Run executes every check.
func Run(p model.Paths) ([]Check, error) {
	return nil, errors.New("not implemented: doctor.Run")
}
