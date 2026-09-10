package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// action is one user-level operation the UI can perform. Keys, the command
// palette and tests all go through this type.
type action int

// Actions, in the order they appear in the help overlay.
const (
	actNone action = iota
	actOpen
	actPreview
	actSearch
	actScope
	actHelp
	actPalette
	actNewHere
	actNewInDir
	actFork
	actKeep
	actNewTab
	actLabel
	actRenameReal
	actPin
	actHide
	actShowHidden
	actTag
	actArchive
	actCopyResume
	actCopyLink
	actStop
	actDelete
	actSetup
	actRefresh
	actGhosts
	actSelectMode
	actUndo
	actQuit
	actUp
	actDown
	actPageUp
	actPageDown
	actHome
	actEnd
)

// actionInfo describes an action for the help overlay and the palette.
type actionInfo struct {
	act   action
	key   string
	label string
	// disabled actions are listed but do nothing.
	disabled bool
	// palette lists the action in the command palette.
	palette bool
}

// actionTable is the single source of truth for keys and labels.
var actionTable = []actionInfo{
	{actOpen, "Enter", "open here (focus, attach or resume)", false, true},
	{actPreview, "Space", "preview (mark in select mode)", false, true},
	{actSearch, "/", "search", false, false},
	{actScope, "Tab", "cycle scope", false, true},
	{actHelp, "?", "help", false, true},
	{actPalette, ":", "command palette", false, false},
	{actNewHere, "n", "new session here", false, true},
	{actNewInDir, "N", "new session in row's dir", false, true},
	{actFork, "f", "fork session", false, true},
	{actKeep, "K", "open and keep (tmux)", false, true},
	{actNewTab, "o", "open in new tab", false, true},
	{actLabel, "r", "rename label", false, true},
	{actRenameReal, "", "real rename (needs claude)", true, true},
	{actPin, "p", "pin / unpin", false, true},
	{actHide, "H", "hide / unhide", false, true},
	{actShowHidden, "h", "show hidden", false, true},
	{actTag, "t", "tag", false, true},
	{actArchive, "a", "archive now", false, true},
	{actCopyResume, "y", "copy resume command", false, true},
	{actCopyLink, "c", "copy claude.ai link", false, true},
	{actStop, "x", "stop (press twice)", false, true},
	{actDelete, "D", "remove from list (press twice)", false, true},
	{actSetup, "S", "setup", false, true},
	{actRefresh, "R", "refresh", false, true},
	{actGhosts, "g", "toggle ghosts", false, true},
	{actSelectMode, "V", "multi-select mode", false, true},
	{actUndo, "u", "undo last hide/remove", false, true},
	{actQuit, "q / Esc", "quit", false, true},
	{actUp, "k / Up", "move up", false, false},
	{actDown, "j / Down", "move down", false, false},
	{actPageUp, "PgUp", "page up", false, false},
	{actPageDown, "PgDn", "page down", false, false},
	{actHome, "Home", "first row", false, false},
	{actEnd, "End", "last row", false, false},
}

// keyToAction maps a key press in the main list to an action.
var keyToAction = map[string]action{
	"enter":     actOpen,
	" ":         actPreview,
	"/":         actSearch,
	"tab":       actScope,
	"?":         actHelp,
	":":         actPalette,
	"n":         actNewHere,
	"N":         actNewInDir,
	"f":         actFork,
	"K":         actKeep,
	"o":         actNewTab,
	"r":         actLabel,
	"R":         actRefresh,
	"p":         actPin,
	"H":         actHide,
	"h":         actShowHidden,
	"t":         actTag,
	"a":         actArchive,
	"y":         actCopyResume,
	"c":         actCopyLink,
	"x":         actStop,
	"D":         actDelete,
	"S":         actSetup,
	"f5":        actRefresh,
	"ctrl+r":    actRefresh,
	"g":         actGhosts,
	"V":         actSelectMode,
	"u":         actUndo,
	"q":         actQuit,
	"esc":       actQuit,
	"ctrl+c":    actQuit,
	"j":         actDown,
	"down":      actDown,
	"k":         actUp,
	"up":        actUp,
	"pgup":      actPageUp,
	"pgdown":    actPageDown,
	"ctrl+u":    actPageUp,
	"ctrl+d":    actPageDown,
	"home":      actHome,
	"end":       actEnd,
	"ctrl+home": actHome,
	"ctrl+end":  actEnd,
}

// actionFor maps a key message to its list action.
func actionFor(msg tea.KeyMsg) action {
	if a, ok := keyToAction[msg.String()]; ok {
		return a
	}
	return actNone
}

// infoFor returns the table entry for an action.
func infoFor(a action) actionInfo {
	for _, i := range actionTable {
		if i.act == a {
			return i
		}
	}
	return actionInfo{act: a}
}

// navigation reports whether an action only moves the cursor. The help
// overlay folds these into one line.
func (a action) navigation() bool {
	switch a {
	case actUp, actDown, actPageUp, actPageDown, actHome, actEnd:
		return true
	}
	return false
}

// destructive reports whether an action needs the confirm-twice guard.
func (a action) destructive() bool {
	return a == actStop || a == actDelete
}

// needsSession reports whether an action operates on the selected row.
func (a action) needsSession() bool {
	switch a {
	case actOpen, actNewInDir, actFork, actKeep, actNewTab, actLabel,
		actRenameReal, actPin, actHide, actTag, actArchive, actCopyResume, actCopyLink,
		actStop, actDelete:
		return true
	}
	return false
}
