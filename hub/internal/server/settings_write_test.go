package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Two settings writes close together both land, and hub.json is whole JSON
// after them with no temporary file left beside it (life-distributable#219).
func TestWriteHubJSONConcurrentWritesKeepBothKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(p, []byte("{\n  \"addr\": \"x\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		var wg sync.WaitGroup
		for _, set := range []map[string]any{{"team_mode": i%2 == 0}, {"model_cap": "opus"}} {
			wg.Add(1)
			go func(set map[string]any) {
				defer wg.Done()
				if err := writeHubJSON(p, set); err != nil {
					t.Error(err)
				}
			}(set)
		}
		wg.Wait()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("hub.json is not whole JSON: %v\n%s", err, b)
		}
		if m["team_mode"] != (i%2 == 0) || m["model_cap"] != "opus" || m["addr"] != "x" {
			t.Fatalf("a write was lost: %s", b)
		}
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", st.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("files left beside hub.json: %v", ents)
	}
}
