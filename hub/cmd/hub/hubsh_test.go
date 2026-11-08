package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `ops/hub.sh url` gives the owner's hub token only to a person at a
// Terminal: run from a session (no tty) it put the token in the session's
// transcript (#129). Run on a copy, against a throwaway hub.json and token.
func TestHubURLKeepsOwnerTokenOutOfSessions(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("hub.sh reads hub.json with plutil (macOS)")
	}
	dir := t.TempDir()
	ops := filepath.Join(dir, "ops")
	os.MkdirAll(filepath.Join(ops, "secrets"), 0o755)
	src, err := os.ReadFile("../../../ops/hub.sh")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(ops, "hub.sh"), src, 0o755)
	os.WriteFile(filepath.Join(ops, "hub.json"), []byte(`{"public_host":"hub.test:8443"}`), 0o644)
	os.WriteFile(filepath.Join(ops, "secrets", "hub.token"), []byte("OWNER-TOKEN\n"), 0o600)

	run := func(env ...string) (string, error) {
		cmd := exec.Command("bash", filepath.Join(ops, "hub.sh"), "url")
		cmd.Stdin = nil // /dev/null: not a tty, as a session's shell
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "LIFE_SESSION_TOKEN=") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run()
	if strings.Contains(out, "OWNER-TOKEN") {
		t.Fatalf("hub.sh url printed the owner's token with no tty: %q", out)
	}
	if err == nil || !strings.Contains(out, "https://hub.test:8443/") {
		t.Fatalf("want the bare URL and a refusal, got %q err=%v", out, err)
	}
	// A session's URL carries a login code, never its token (#161): with no
	// hub at hub.test there is no code, so it says so rather than fall back
	// to the token.
	out, err = run("LIFE_SESSION_TOKEN=sess-tok")
	if err == nil || strings.Contains(out, "sess-tok") || !strings.Contains(out, "no login code") {
		t.Fatalf("a session with no hub: got %q err=%v", out, err)
	}
}
