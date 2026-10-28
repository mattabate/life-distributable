package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// "Today" in the hub is the owner's day (store.Day), never the process zone:
// time.Now().Format("2006-01-02") dates a row a day off between midnight and
// the zone offset on a Mac set to UTC, or on any CI runner. This walks every
// non-test .go file under hub/internal and fails on the pattern, so the rule
// holds without anyone remembering it.
func TestTodayIsAlwaysTheOwnersDay(t *testing.T) {
	root := filepath.Join("..")
	var bad []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `time.Now().Format("2006-01-02")`) || strings.Contains(line, `time.Now().UTC().Format("2006-01-02")`) {
				bad = append(bad, p+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("today as the process zone's day; use store.Day(time.Now()):\n%s", strings.Join(bad, "\n"))
	}
}
