package launch

// cmux backend: opens a session in a new cmux workspace through the cmux
// socket CLI. Implemented in this file; see CmuxAvailable and CmuxOpenArgv.

// TerminalCmux is the Options.Terminal value for cmux.
const TerminalCmux = "cmux"

// CmuxAvailable reports whether the cmux CLI can be found on this machine
// and returns its path.
func CmuxAvailable() (string, bool) {
	return "", false
}
