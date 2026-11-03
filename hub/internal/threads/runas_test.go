package threads

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAsOffIsPlainTmux(t *testing.T) {
	var got string
	m := &Manager{ClaudeBin: "/hub/claude", Run: func(n string, a ...string) ([]byte, error) {
		got = strings.Join(append([]string{n}, a...), " ")
		return nil, nil
	}}
	m.tmux("has-session", "-t", "x")
	if got != "tmux has-session -t x" {
		t.Fatalf("off: %q", got)
	}
	if m.sessionClaudeBin() != "/hub/claude" || m.sessionUserEnv() != "" {
		t.Fatalf("off: bin %q env %q", m.sessionClaudeBin(), m.sessionUserEnv())
	}
	if d := m.sessionDir(Thread{ID: "a"}, "/repo"); d != "/repo" {
		t.Fatalf("off: dir %q", d)
	}
}

func TestRunAsOnGoesThroughSudoAndNeverReadsTheToken(t *testing.T) {
	var got string
	m := &Manager{ClaudeBin: "/hub/claude", RunAs: RunAs{User: "life-agent", ClaudeBin: "/agent/claude", TmuxBin: "/brew/tmux"},
		Run: func(n string, a ...string) ([]byte, error) {
			got = strings.Join(append([]string{n}, a...), " ")
			return nil, nil
		}}
	m.tmux("kill-session", "-t", "x")
	// -n: a sudo that wants a password fails at once instead of hanging the hub.
	if got != "/usr/bin/sudo -n -H -u life-agent /brew/tmux kill-session -t x" {
		t.Fatalf("on: %q", got)
	}
	if m.sessionClaudeBin() != "/agent/claude" {
		t.Fatalf("on: bin %q", m.sessionClaudeBin())
	}
	// The session user's shell reads the file: the value is never in the hub.
	if env := m.sessionUserEnv(); !strings.Contains(env, `"$(cat "$HOME/.claude-token")"`) {
		t.Fatalf("on: env %q", env)
	}
}

// As the session user, a session of the main checkout starts in a clone of its
// own on branch th-<id>, made and linked as that user, never with ops/secrets;
// the process runs there through sudo with its own token. A session of
// another project runs where it always did.
func TestSessionCloneAsSessionUser(t *testing.T) {
	dir := t.TempDir()
	m, _, _ := setupAt(t, dir)
	os.MkdirAll(filepath.Join(dir, "ops", "secrets"), 0o700)
	os.MkdirAll(filepath.Join(dir, "data"), 0o700)
	os.WriteFile(filepath.Join(dir, "ops", "hub.json"), []byte("{}"), 0o600)
	clones := filepath.Join(t.TempDir(), "worktrees")
	m.RunAs = RunAs{User: "life-agent", ClaudeBin: "/agent/claude", TmuxBin: "/brew/tmux", CloneRoot: clones, Repo: dir}
	m.SessionToken = func(id string) string { return "s1." + id }
	var cmds []string
	m.Run = func(n string, a ...string) ([]byte, error) {
		c := strings.Join(append([]string{n}, a...), " ")
		if strings.Contains(c, "has-session") {
			return nil, nil
		}
		cmds = append(cmds, c)
		if strings.Contains(c, " test -d ") || strings.Contains(c, "checkout --quiet th-") {
			return nil, errors.New("no")
		}
		return nil, nil
	}
	th, err := m.Create("", "life", "", "Hello", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(clones, "th-"+th.ID)
	all := strings.Join(cmds, "\n")
	sudo := "/usr/bin/sudo -n -H -u life-agent "
	for _, want := range []string{
		sudo + "git clone --quiet --no-hardlinks " + dir + " " + wt,
		sudo + "git -C " + wt + " checkout --quiet -b th-" + th.ID + " origin/main",
		sudo + "ln -s " + filepath.Join(dir, "data") + " " + filepath.Join(wt, "data"),
		sudo + "ln -s " + filepath.Join(dir, "ops", "hub.json") + " " + filepath.Join(wt, "ops", "hub.json"),
		sudo + "mkdir -p " + filepath.Join(wt, "ops", "logs"),
		sudo + "/brew/tmux new-session -d -s ",
		"-c " + wt + " ",
		"LIFE_SESSION_TOKEN='s1." + th.ID + "'",
		"'/agent/claude'",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	for _, c := range cmds {
		if strings.Contains(c, " ln ") && strings.Contains(c, "secrets") {
			t.Errorf("a clone links ops/secrets: %s", c)
		}
	}

	// A second start finds the clone and catches it up instead.
	cmds = nil
	m.Run = func(n string, a ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(append([]string{n}, a...), " "))
		return nil, nil
	}
	if d := m.sessionDir(th, dir); d != wt {
		t.Fatalf("second start: %q", d)
	}
	if got := strings.Join(cmds, "\n"); !strings.Contains(got, sudo+"git -C "+wt+" merge --ff-only --quiet origin/main") || strings.Contains(got, "clone") {
		t.Errorf("second start:\n%s", got)
	}

	// A clone with unlanded commits never fast-forwards, so it kept the
	// scripts it was made with: it is rebased onto main, and a rebase that
	// stops is aborted.
	for _, rebaseFails := range []bool{false, true} {
		cmds = nil
		m.Run = func(n string, a ...string) ([]byte, error) {
			line := strings.Join(append([]string{n}, a...), " ")
			cmds = append(cmds, line)
			if strings.Contains(line, " merge --ff-only ") || (rebaseFails && strings.Contains(line, " rebase --quiet ")) {
				return nil, os.ErrInvalid
			}
			return nil, nil
		}
		m.catchUp(th, wt)
		want := []string{
			sudo + "git -C " + wt + " fetch --quiet origin main",
			sudo + "git -C " + wt + " merge --ff-only --quiet origin/main",
			sudo + "git -C " + wt + " rebase --quiet origin/main",
		}
		if rebaseFails {
			want = append(want, sudo+"git -C "+wt+" rebase --abort")
		}
		if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
			t.Errorf("diverged clone (rebase fails %v): %v", rebaseFails, cmds)
		}
	}
	m.Run = func(n string, a ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(append([]string{n}, a...), " "))
		return nil, nil
	}

	// Another project's directory is not cloned.
	cmds = nil
	if d := m.sessionDir(Thread{ID: "b"}, "/elsewhere"); d != "/elsewhere" || len(cmds) != 0 {
		t.Errorf("other project: %q %v", d, cmds)
	}
}
