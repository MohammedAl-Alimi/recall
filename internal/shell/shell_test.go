package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWidgetShapes(t *testing.T) {
	for _, sh := range Supported {
		w := Widget(sh)
		if !strings.HasPrefix(w, BeginMarker+"\n") || !strings.HasSuffix(w, EndMarker+"\n") {
			t.Fatalf("%s: markers missing:\n%s", sh, w)
		}
		if !strings.Contains(w, "recall --from-widget") {
			t.Fatalf("%s: no widget command", sh)
		}
		if !strings.Contains(w, "alias rcl='recall'") {
			t.Fatalf("%s: no rcl alias", sh)
		}
		if strings.ContainsRune(w, 0x2014) {
			t.Fatalf("%s: em dash in snippet", sh)
		}
	}
	if !strings.Contains(Widget("zsh"), "bindkey '^G'") || !strings.Contains(Widget("zsh"), "zle -N _recall_widget") {
		t.Fatal("zsh binding missing")
	}
	if !strings.Contains(Widget("bash"), `bind -x '"\C-g"`) {
		t.Fatal("bash binding missing")
	}
	if !strings.Contains(Widget("fish"), `bind \cg _recall_widget`) {
		t.Fatal("fish binding missing")
	}
	if Widget("tcsh") != "" || Widget("") != "" {
		t.Fatal("unknown shell should yield empty")
	}
	if Widget("/bin/zsh") != Widget("zsh") {
		t.Fatal("full path should normalise")
	}
}

func TestInstallAppendsOnce(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("export FOO=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Install("zsh", rc); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rc)
	want := "export FOO=1\n\n" + Widget("zsh")
	if string(got) != want {
		t.Fatalf("after install:\n%s", got)
	}
	st, _ := os.Stat(rc)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
	if !Installed(rc) {
		t.Fatal("Installed false after install")
	}
	if err := Install("zsh", rc); err != nil {
		t.Fatal(err)
	}
	got2, _ := os.ReadFile(rc)
	if string(got2) != want {
		t.Fatalf("second install changed file:\n%s", got2)
	}
	if strings.Count(string(got2), BeginMarker) != 1 {
		t.Fatal("block duplicated")
	}
}

func TestInstallReplacesStaleBlock(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	stale := "a=1\n\n" + BeginMarker + "\nold stuff\n" + EndMarker + "\nb=2\n"
	if err := os.WriteFile(rc, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install("bash", rc); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rc)
	want := "a=1\n\n" + Widget("bash") + "b=2\n"
	if string(got) != want {
		t.Fatalf("replace:\n%s", got)
	}
}

func TestInstallCreatesMissingFile(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "fish", "config.fish")
	if err := Install("fish", rc); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rc)
	if string(got) != Widget("fish") {
		t.Fatalf("created:\n%s", got)
	}
	if err := Install("tcsh", rc); err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if err := Install("zsh", ""); err == nil {
		t.Fatal("expected error for empty rc path")
	}
}

func TestUninstall(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	orig := "export FOO=1\n"
	if err := os.WriteFile(rc, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install("zsh", rc); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(rc); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rc)
	if string(got) != orig {
		t.Fatalf("uninstall left:\n%q", got)
	}
	if Installed(rc) {
		t.Fatal("Installed true after uninstall")
	}
	// idempotent and safe on missing file
	if err := Uninstall(rc); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
	// block in the middle
	mid := "a=1\n" + Widget("zsh") + "b=2\n"
	if err := os.WriteFile(rc, []byte(mid), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(rc); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(rc)
	if string(got) != "a=1\nb=2\n" {
		t.Fatalf("middle uninstall left:\n%q", got)
	}
}

func TestDetect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	t.Setenv("SHELL", "/bin/zsh")
	sh, rc := Detect()
	if sh != "zsh" || rc != filepath.Join(home, ".zshrc") {
		t.Fatalf("zsh: %s %s", sh, rc)
	}
	t.Setenv("ZDOTDIR", filepath.Join(home, "zdot"))
	if _, rc = Detect(); rc != filepath.Join(home, "zdot", ".zshrc") {
		t.Fatalf("zdotdir: %s", rc)
	}

	t.Setenv("SHELL", "/opt/homebrew/bin/bash")
	sh, rc = Detect()
	if sh != "bash" || rc != filepath.Join(home, ".bashrc") {
		t.Fatalf("bash: %s %s", sh, rc)
	}
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rc = Detect(); rc != filepath.Join(home, ".bash_profile") {
		t.Fatalf("bash_profile preferred when only it exists: %s", rc)
	}

	t.Setenv("SHELL", "/usr/local/bin/fish")
	sh, rc = Detect()
	if sh != "fish" || rc != filepath.Join(home, ".config", "fish", "config.fish") {
		t.Fatalf("fish: %s %s", sh, rc)
	}

	t.Setenv("SHELL", "/bin/tcsh")
	if sh, rc = Detect(); sh != "" || rc != "" {
		t.Fatalf("tcsh: %q %q", sh, rc)
	}
}

// TestSnippetsParse feeds each snippet to its shell's syntax checker when
// that shell is installed. Nothing is executed or sourced.
func TestSnippetsParse(t *testing.T) {
	cases := map[string][]string{
		"zsh":  {"zsh", "-n"},
		"bash": {"bash", "-n"},
		"fish": {"fish", "--no-execute"},
	}
	for sh, cmd := range cases {
		bin, err := exec.LookPath(cmd[0])
		if err != nil {
			t.Logf("%s not installed, skipping parse check", sh)
			continue
		}
		f := filepath.Join(t.TempDir(), "snippet")
		if err := os.WriteFile(f, []byte(Widget(sh)), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(bin, append(cmd[1:], f)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s rejected snippet: %v\n%s", sh, err, out)
		}
	}
}
