package threads

import (
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
)

// A session's `lifectl` call refused by its own tool grant is a broken
// install, not something for the owner to approve: they have no terminal
// running the session and no settings file to edit (#265). The CLI lists a
// turn's refused calls in its result line (permission_denials); the first
// lifectl one raises a single error card on that session, which names the
// command, instead of the session asking the owner to do engineering.

// grantRefusedTitle: one card for every such refusal, raised once.
const grantRefusedTitle = "Sessions cannot run lifectl: their tool grant refuses it"

// refusedLifectl: the first refused Bash call in a result line whose command
// is lifectl (bare or by path), "" if none.
func refusedLifectl(line string) string {
	var env struct {
		Denials []struct {
			Tool  string `json:"tool_name"`
			Input struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		} `json:"permission_denials"`
	}
	if json.Unmarshal([]byte(line), &env) != nil {
		return ""
	}
	for _, d := range env.Denials {
		f := strings.Fields(d.Input.Command)
		if d.Tool == "Bash" && len(f) > 0 && filepath.Base(f[0]) == "lifectl" {
			return truncate(strings.Join(f, " "), 160)
		}
	}
	return ""
}

// grantRefused raises the card on the session's thread for a turn whose
// lifectl call was refused, unless that thread ever carried it (a dismissed
// one stays closed).
func (m *Manager) grantRefused(threadID, line string) {
	cmd := refusedLifectl(line)
	if cmd == "" || m.AskSeen(threadID, grantRefusedTitle) {
		return
	}
	detail := "This session ran `" + cmd + "` and its tool grant refused it, so it could not reach the hub. This is the install, not a tool to approve.\n\n" +
		"1. Check that lifectl is on the sessions' PATH: it is built into `ops/bin` by `make build`, and a session gets that folder first on its PATH from the hub.\n" +
		"2. Check that `prompt_tools` in `ops/schedule.json` still holds `Bash(lifectl:*)`.\n" +
		"3. Run `ops/hub.sh restart`; each session starts fresh on its next message."
	if _, err := m.AddAsk(threadID, "", grantRefusedTitle, detail, "error", ""); err != nil {
		log.Printf("thread %s: refused lifectl card: %v", threadID, err)
	}
}
