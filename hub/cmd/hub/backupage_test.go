package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A night launchd starts no backup raised no ask (#151). The hub's check is
// due only when the newest clean-finish stamp is over 36 h old, and the title
// carries that stamp's day.
func TestBackupAgeAskOnlyWhenNothingFinished(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	dir := t.TempDir()
	stamp := func(name string, ago time.Duration) {
		os.WriteFile(filepath.Join(dir, name+".ok"), []byte(strconv.FormatInt(now.Add(-ago).Unix(), 10)+"\n"), 0o644)
	}
	if _, _, due := backupAgeAsk(dir, now); due {
		t.Fatal("no stamps yet (a fresh install) raised an ask")
	}
	stamp("backup", 8*time.Hour)
	if _, _, due := backupAgeAsk(dir, now); due {
		t.Fatal("the backup finished 8 h ago, still due")
	}
	stamp("backup", 40*time.Hour)
	title, detail, due := backupAgeAsk(dir, now)
	if !due {
		t.Fatal("the stamp is over 36 h old, not due")
	}
	if want := now.Add(-40 * time.Hour).Format("Jan 2, 2006"); !strings.HasSuffix(title, want) {
		t.Fatalf("title %q does not end with the newest stamp's day %s", title, want)
	}
	for _, s := range []string{"backup ", "backup.log"} {
		if !strings.Contains(detail, s) {
			t.Fatalf("detail misses %s: %q", s, detail)
		}
	}
	// A stamp that does not parse counts as none.
	os.WriteFile(filepath.Join(dir, "backup.ok"), []byte("garbage"), 0o644)
	if _, _, due := backupAgeAsk(dir, now); due {
		t.Fatal("an unreadable stamp raised an ask")
	}
}
