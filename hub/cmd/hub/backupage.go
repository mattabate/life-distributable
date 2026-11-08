package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// backupStale: how old the newest clean-finish stamp may get before the hub
// raises its ask. The backup runs nightly (03:30), so 36 hours means at least
// one whole night finished nothing.
const backupStale = 36 * time.Hour

// backupStamps: the ops/logs/<name>.ok files (epoch seconds) a backup job
// writes when it finishes clean. ops/backup.sh writes backup.ok as its last
// line; a job added later appends its own name here.
var backupStamps = []string{"backup"}

// backupAgeAsk says what to ask the owner when no nightly backup has finished
// clean in backupStale. backup.sh's own trap raises an ask only when it runs
// and fails while the hub is up; a night where launchd starts nothing (the
// Mac asleep, a broken plist) or a failure while the hub is down raised
// nothing (#151). The title carries the newest stamp's day, so one stall is
// one ask (AskSeen) and a later stall gets its own. No stamps at all (a fresh
// install, or backups never set up) raises nothing.
func backupAgeAsk(logs string, now time.Time) (title, detail string, due bool) {
	var newest time.Time
	var seen []string
	for _, name := range backupStamps {
		b, err := os.ReadFile(filepath.Join(logs, name+".ok"))
		if err != nil {
			continue
		}
		sec, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			continue
		}
		t := time.Unix(sec, 0)
		seen = append(seen, name+" "+t.Local().Format("Mon Jan 2 15:04"))
		if t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() || now.Sub(newest) <= backupStale {
		return "", "", false
	}
	title = "No nightly backup has finished since " + newest.Local().Format("Jan 2, 2006")
	detail = "Last clean finish (ops/logs/<job>.ok): " + strings.Join(seen, "; ") +
		". Nothing ran or every run failed: check the Mac was awake, `launchctl list | grep com.life.backup` (`ops/hub.sh install-backup` reloads it), and ops/logs/backup.log."
	return title, detail, true
}
