package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
