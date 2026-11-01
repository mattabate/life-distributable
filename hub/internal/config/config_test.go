package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only a named interface passes: the hub binds to Tailscale, nothing wider.
func TestBindsAll(t *testing.T) {
	for addr, want := range map[string]bool{
		":8443": true, "0.0.0.0:8443": true, "[::]:8443": true, "[0:0:0:0:0:0:0:0]:8443": true, "[::]": true,
		"100.64.0.1:8443": false, "[fd7a:115c:a1e0::1]:8443": false, "mini.tail.ts.net:8443": false,
	} {
		if bindsAll(addr) != want {
			t.Errorf("bindsAll(%q) = %v", addr, !want)
		}
	}
}

func TestOnTailnet(t *testing.T) {
	for addr, want := range map[string]bool{
		"100.64.0.1:8443": true, "100.127.255.255:8443": true, "[fd7a:115c:a1e0::1]:8443": true, "mini.tail.ts.net:8443": true,
		"192.168.1.20:8443": false, "10.0.0.5:8443": false, "100.63.255.255:8443": false, "100.128.0.0:8443": false,
		"127.0.0.1:8443": false, "[fe80::1]:8443": false, "example.com:8443": false, "evil.ts.net.example.com:8443": false,
	} {
		if onTailnet(addr) != want {
			t.Errorf("onTailnet(%q) = %v", addr, !want)
		}
	}
}

// A bad hub.json names every problem in one error, so one restart fixes it.
func TestLoadNamesEveryProblemAtOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.json")
	os.WriteFile(p, []byte(`{"listen_addr": ":8443"}`), 0o644)
	_, err := Load(p)
	if err == nil {
		t.Fatal("an empty config loaded")
	}
	for _, want := range []string{"claude_bin required", "db_path required", `listen_addr ":8443" would bind all interfaces`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if strings.Count(err.Error(), "; ") != 2 {
		t.Errorf("want three problems on one line: %v", err)
	}
}

// Session tokens are on unless hub.json turns them off by name.
func TestSessionTokensDefaultOn(t *testing.T) {
	dir := t.TempDir()
	for extra, want := range map[string]bool{
		``:                          true,
		`, "session_tokens": true`:  true,
		`, "session_tokens": false`: false,
	} {
		p := filepath.Join(dir, "hub.json")
		os.WriteFile(p, []byte(`{"listen_addr": "100.64.0.1:8443", "claude_bin": "/bin/sh", "db_path": "x"`+extra+`}`), 0o644)
		c, err := Load(p)
		if err != nil {
			t.Fatalf("%q: %v", extra, err)
		}
		if c.SessionTokens != want {
			t.Errorf("%q: session_tokens = %v, want %v", extra, c.SessionTokens, want)
		}
	}
}

// session_user is off when empty; set, its paths default from its home, and it
// refuses a name sudo would read as a flag or a config without session tokens.
func TestSessionUserDefaultsAndRefusals(t *testing.T) {
	dir := t.TempDir()
	load := func(extra string) (*Config, error) {
		p := filepath.Join(dir, "hub.json")
		os.WriteFile(p, []byte(`{"listen_addr": "100.64.0.1:8443", "claude_bin": "/bin/sh", "db_path": "x"`+extra+`}`), 0o644)
		return Load(p)
	}
	c, err := load(``)
	if err != nil || c.SessionUser != "" || c.SessionWorktrees != "" {
		t.Fatalf("off by default: %+v %v", c, err)
	}
	c, err = load(`, "session_user": "sessions"`)
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionClaudeBin != "/Users/sessions/.local/bin/claude" || c.SessionTmuxBin != "/opt/homebrew/bin/tmux" ||
		c.SessionWorktrees != "/Users/sessions/worktrees" || c.SessionProjectsDir != "/Users/sessions/.claude/projects" {
		t.Errorf("defaults: %+v", c)
	}
	c, err = load(`, "session_user": "life-agent", "session_worktrees": "/Volumes/x/clones"`)
	if err != nil || c.SessionWorktrees != "/Volumes/x/clones" {
		t.Errorf("an explicit path is kept: %v %v", c, err)
	}
	for extra, want := range map[string]string{
		`, "session_user": "-u root"`:                                "not a macOS short name",
		`, "session_user": "life-agent", "session_tokens": false`:    "needs session_tokens",
		`, "session_user": "life-agent", "session_tmux_bin": "tmux"`: "absolute",
	} {
		if _, err := load(extra); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", extra, want, err)
		}
	}
}

// #55: time_zone names the household's clock; a name Go cannot load is
// refused at start-up, not discovered at midnight. Empty is the Mac's zone.
func TestTimeZoneIsValidated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.json")
	os.WriteFile(p, []byte(`{"listen_addr": ":8443", "time_zone": "Nowhere/Land"}`), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), `time_zone "Nowhere/Land"`) {
		t.Fatalf("a bad time_zone loaded: %v", err)
	}
	if loc, err := (&Config{}).Zone(); err != nil || loc != time.Local {
		t.Fatalf("empty time_zone = %v, %v; want the Mac's own", loc, err)
	}
}
