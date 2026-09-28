package sessions

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestListParsesOnlyOwned(t *testing.T) {
	m := New("life", t.TempDir(), "/usr/local/bin/claude")
	m.Run = func(n string, a ...string) ([]byte, error) {
		return []byte("life-rc-health|1755600000|0\nscratch|1|1\nlife-job-x|1755600001|0\n"), nil
	}
	ss, err := m.List()
	if err != nil || len(ss) != 2 || ss[0].Kind != "job" || ss[1].Project != "health" {
		t.Fatalf("%+v %v", ss, err)
	}
}

func TestListNoServer(t *testing.T) {
	m := New("life", t.TempDir(), "/usr/local/bin/claude")
	m.Run = func(n string, a ...string) ([]byte, error) { return []byte("no server running on /x"), errors.New("x") }
	ss, err := m.List()
	if err != nil || len(ss) != 0 {
		t.Fatal(ss, err)
	}
}

func TestStartRemoteControlCommand(t *testing.T) {
	m := New("life", t.TempDir(), "/usr/local/bin/claude")
	var got []string
	m.Sleep = func(time.Duration) {}
	m.Run = func(n string, a ...string) ([]byte, error) {
		switch a[0] {
		case "has-session":
			return nil, errors.New("none")
		case "capture-pane":
			return []byte("Connected\n"), nil
		}
		got = append([]string{n}, a...)
		return nil, nil
	}
	s, created, err := m.StartRemoteControl("health", "/tmp/h")
	if err != nil || !created || s.Name != "life-rc-health" {
		t.Fatal(s, created, err)
	}
	if strings.Join(got, " ") != "tmux new-session -d -s life-rc-health -c /tmp/h '/usr/local/bin/claude' remote-control --spawn same-dir --name 'life/health' || { echo \"[exited $?]\"; sleep 5; }" {
		t.Fatalf("%q", strings.Join(got, " "))
	}
	if _, _, err := m.StartRemoteControl("../evil", "/"); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestKillOnlyOwned(t *testing.T) {
	m := New("life", t.TempDir(), "/usr/local/bin/claude")
	m.Run = func(n string, a ...string) ([]byte, error) { return nil, nil }
	if err := m.Kill("scratch"); err == nil {
		t.Fatal("killed foreign session")
	}
	if err := m.Kill("life-rc-x"); err != nil {
		t.Fatal(err)
	}
}

func TestJobLifecycle(t *testing.T) {
	m := New("life", t.TempDir(), "/usr/local/bin/claude")
	m.Run = func(n string, a ...string) ([]byte, error) { return nil, nil }
	j, err := m.StartJob("life", "/tmp", "say hi")
	if err != nil {
		t.Fatal(err)
	}
	js, _ := m.Jobs()
	if len(js) != 1 || js[0].Done || js[0].ID != j.ID {
		t.Fatalf("%+v", js)
	}
}

func TestStartRemoteControlReportsEarlyExit(t *testing.T) {
	m := New("life", t.TempDir(), "/usr/local/bin/claude")
	m.Sleep = func(time.Duration) {}
	killed := false
	m.Run = func(n string, a ...string) ([]byte, error) {
		switch a[0] {
		case "has-session":
			return nil, errors.New("none")
		case "capture-pane":
			return []byte("\n\nError: Workspace not trusted.\n[exited 1]\n"), nil
		case "kill-session":
			killed = true
		}
		return nil, nil
	}
	_, _, err := m.StartRemoteControl("health", "/tmp/h")
	if err == nil || !strings.Contains(err.Error(), "Workspace not trusted") || !killed {
		t.Fatal(err, killed)
	}
}
