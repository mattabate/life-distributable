package threads

// Model preflight: a new model can go on the ladder while the installed CLI
// does not know it yet, and nothing notices until turns die. At startup the
// hub asks each ladder rung for one word from the exact binary sessions use;
// a rung the CLI rejects is closed before any turn lands on it, and the owner
// gets one card saying the CLI needs updating. The
// reactive half — a live turn that hits the same 400 steps down the ladder —
// is relaunchBelow in runner.go.

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"life/hub/internal/spend"
)

// probeModel runs one tool-less turn on `model` and returns what the CLI said.
func (m *Manager) probeModel(model string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := m.sessionCLI(ctx, "-p", "--model", model, "--output-format", "text", "--max-turns", "1", "--tools", "")
	c.Stdin = strings.NewReader("Reply with the single word OK.")
	out, err := c.CombinedOutput()
	return string(out), err
}

// PreflightModels probes every rung and returns the ones the CLI rejects as
// unsupported. Each is closed on the picker (ModelExhausted) and raised once
// as a card on the hub thread. Any other failure (network, a limit) is only
// logged: the ladder's reactive path already handles those.
func (m *Manager) PreflightModels(ladder []string) []string {
	if m.Probe == nil {
		return nil
	}
	// Only a CLI update (or a new ladder) can change the answer, and probing
	// every rung on every restart adds up. The key is stored only after a
	// clean pass, so a rejected rung or a failed probe is retried on the next
	// restart.
	key := ""
	if m.CLIVersion != nil {
		if v := m.CLIVersion(); v != "" {
			key = v + " | " + strings.Join(ladder, ",")
		}
	}
	if key != "" && m.db.Setting(preflightKey) == key {
		log.Printf("preflight: skipped, already passed on %s", key)
		return nil
	}
	var bad []string
	clean := true
	for _, model := range ladder {
		out, err := m.Probe(model)
		if err == nil && !spend.IsUnsupportedModelError(out) {
			log.Printf("preflight: %s ok", model)
			continue
		}
		clean = false
		if !spend.IsUnsupportedModelError(out) {
			log.Printf("preflight: %s failed (%v): %s", model, err, firstLine(out, 200))
			continue
		}
		log.Printf("preflight: CLI rejects %s: %s", model, firstLine(out, 200))
		bad = append(bad, model)
		if m.ModelExhausted != nil {
			m.ModelExhausted(model)
		}
		m.raiseStaleCLI(model, out)
	}
	if clean && key != "" {
		m.db.SetSetting(preflightKey, key)
	}
	return bad
}

// preflightKey: the settings row holding the CLI version + ladder that last
// passed every probe.
const preflightKey = "preflight:passed"

// cliVersion is `claude --version`: no model call, no tokens.
func (m *Manager) cliVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := m.sessionCLI(ctx, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sessionCLI: the CLI sessions run, as the user they run as, with that user's
// sign-in. Preflight had asked the hub user's own CLI, which updates on its
// own schedule, so a rung could pass here and be refused in every session.
// The session user cannot enter the hub user's temp folder, so it runs from
// an empty one of its own.
func (m *Manager) sessionCLI(ctx context.Context, args ...string) *exec.Cmd {
	if m.RunAs.User == "" {
		c := exec.CommandContext(ctx, m.ClaudeBin, args...)
		c.Dir = sideDir()
		return c
	}
	sh := `d=$(mktemp -d) || exit 1; cd "$d" || exit 1;` + m.sessionUserEnv() + " " + shellQuote(m.RunAs.ClaudeBin)
	for _, a := range args {
		sh += " " + shellQuote(a)
	}
	sh += `; rc=$?; cd /; rmdir "$d"; exit $rc`
	c := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "-H", "-u", m.RunAs.User, "/bin/sh", "-c", sh)
	c.Dir = "/"
	return c
}

// sideDir is an empty folder the tool-less one-shot calls (preflight probes,
// titles and command descriptions) run from, so the CLI loads no project
// instructions and no memory for a one-word answer.
func sideDir() string {
	d := filepath.Join(os.TempDir(), "life-side-calls")
	if os.MkdirAll(d, 0o700) != nil {
		return os.TempDir()
	}
	return d
}

func (m *Manager) raiseStaleCLI(model, out string) {
	title := "Update the Claude CLI: it rejects " + model
	if open, err := m.ListAsks("active", "hub", 200); err == nil {
		for _, a := range open {
			if a.Title == title {
				return
			}
		}
	}
	update := "`claude update`"
	if m.RunAs.User != "" {
		update = "`sudo -u " + m.RunAs.User + " -H " + m.RunAs.ClaudeBin + " update`"
	}
	detail := fmt.Sprintf("The hub's startup check ran `%s -p --model %s` and the CLI refused the model, so sessions skip that rung and run one rung lower until it is fixed.\n\n1. Open Terminal on the Mac.\n2. Type %s and press Return.\n3. Type `ops/hub.sh restart` from `~/life` so the check runs again.\n\nWhat the CLI said:\n\n```\n%s\n```", m.sessionClaudeBin(), model, update, truncate(strings.TrimSpace(out), 600))
	say := m.hey() + fmt.Sprintf("the Claude command line tool is too old for %s, so sessions are running on the next model down. Please update it.", ModelLabel(model))
	if _, err := m.AddAskSaid("hub", "", title, detail, "physical", "the hub log shows `preflight: "+model+" ok` after a restart", "web", say); err != nil {
		log.Printf("preflight: ask: %v", err)
	}
}
