package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Stdout receives dry-run output. Tests swap it for a buffer.
var Stdout io.Writer = os.Stdout

// Timeout bounds a single osascript call so the UI never hangs on it.
var Timeout = 5 * time.Second

// runner executes the notification script. Tests replace it.
var runner = runOsascript

// Send shows a desktop notification with the given title and body.
// With RECALL_DRY_RUN=1 it only prints the notification.
func Send(title, body string) error {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return errors.New("notify: empty notification")
	}
	script := Script(title, body)
	if os.Getenv("RECALL_DRY_RUN") == "1" {
		fmt.Fprintf(Stdout, "DRY RUN: notify title=%q body=%q\n", title, body)
		return nil
	}
	return runner(script)
}

// Script returns the AppleScript 'display notification' statement.
func Script(title, body string) string {
	s := "display notification " + appleString(body)
	if title != "" {
		s += " with title " + appleString(title)
	}
	return s
}

func runOsascript(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("notify: osascript: %s", msg)
		}
		return fmt.Errorf("notify: osascript: %w", err)
	}
	return nil
}

// appleString quotes s as an AppleScript string literal.
func appleString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
