// Package config loads ops/hub.json — the single place that names projects,
// ports and secret paths. Keep it boring: plain JSON, no env-var magic.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"life/hub/internal/spend"
)

type Project struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type Config struct {
	// ListenAddr is the Tailscale IP:port. Never 0.0.0.0 — see DESIGN.md.
	ListenAddr string `json:"listen_addr"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	TokenFile  string `json:"token_file"`
	// ClaudeProjectsDir is where Claude Code writes transcripts.
	ClaudeProjectsDir string `json:"claude_projects_dir"`
	// JobsDir stores results of headless spawn-with-prompt runs.
	JobsDir string `json:"jobs_dir"`
	// ProjectsRoot: every subdirectory becomes a project automatically.
	ProjectsRoot string    `json:"projects_root"`
	Projects     []Project `json:"projects"`
	// TmuxPrefix namespaces the tmux sessions the hub owns.
	TmuxPrefix string `json:"tmux_prefix"`
	// ClaudeBin is the absolute path to the claude CLI. launchd's PATH does
	// not include nvm/npm bins, so never rely on lookup.
	ClaudeBin string `json:"claude_bin"`
	// OwnerName is what the agent calls the person it works for (session
	// preamble, cards). Empty = "the owner".
	OwnerName string `json:"owner_name"`
	// ModelLadder: models for sessions, best first; a run moves down a rung
	// when the rung's plan bucket is full. Empty = spend.DefaultLadder.
	ModelLadder []string `json:"model_ladder"`
	// ModelStepDown[i]: utilization % (shared 5h/7d or the rung's own bucket)
	// at which rung i closes, so the ladder steps down BEFORE a limit is
	// reached. Empty = spend.DefaultStepDown (85, 95).
	ModelStepDown []float64 `json:"model_step_down"`
	// ModelPolicy: which wakes run on which rung, at what effort, with what
	// caps, plus the daily budget (spend/policy.go). Missing pieces fall back
	// to spend.DefaultPolicy(); nil = the default policy entirely.
	ModelPolicy *spend.Policy `json:"model_policy"`
	// AutoCompactWindow caps how large a session's context may grow before the
	// CLI compacts it, in tokens (exported as CLAUDE_CODE_AUTO_COMPACT_WINDOW
	// on every thread run). Threads `--resume` forever, so without a cap they
	// grow monotonically and every later turn re-reads the whole transcript at
	// the cache-read rate — measured 2026-08-25: zero compactions had EVER
	// fired, because `opus[1m]` gives a 1M window and auto-compact only trips
	// near it, while the median hub turn was already re-reading 339k tokens.
	// The CLI clamps this to 100k..1M. 0 = leave the CLI's own default alone.
	AutoCompactWindow int `json:"auto_compact_window"`
	// DBPath is the SQLite file (under data/, backed up nightly).
	DBPath string `json:"db_path"`
	// SchedulePath is the cron table (ops/schedule.json).
	SchedulePath string `json:"schedule_path"`
	// BlobDir: content-addressed photo/file store (under data/).
	BlobDir string `json:"blob_dir"`
	// APNs push (needs a paid Apple Developer team). Key file, key id, team
	// id and bundle id are all needed to enable it; the .p8 key lives under
	// ops/secrets/. apns_production=false → sandbox host, which is what
	// development-signed (install-phone) builds receive from.
	APNsKeyFile    string `json:"apns_key_file"`
	APNsKeyID      string `json:"apns_key_id"`
	APNsTeamID     string `json:"apns_team_id"`
	APNsProduction bool   `json:"apns_production"`
	// APNsBundleID is the app's bundle id (the push topic) — the same value
	// as LIFE_BUNDLE_ID in app/local.yml.
	APNsBundleID string `json:"apns_bundle_id"`
	// PublicHost is host[:port] the phone reaches the hub at (the Tailscale
	// MagicDNS name + port); used to mint links such as OAuth callbacks.
	PublicHost string `json:"public_host"`
	// UsageOptIn: the setup answer to "send an anonymous weekly heartbeat?"
	// (hub/internal/usage). Recorded once; Settings is the switch after that.
	// UsageHost/UsageToken: empty = the distributable's PostHog project.
	UsageOptIn bool   `json:"usage_opt_in"`
	UsageHost  string `json:"usage_host"`
	UsageToken string `json:"usage_token"`
	// Surfaces: the setup answer to "which apps?" (phone, desktop, web);
	// empty = all three. `setup.py show` reads it to list what is left.
	Surfaces []string `json:"surfaces"`
	// Root is the repo checkout (~/life), derived at load time from the
	// config file's location (<root>/ops/hub.json); never written in the file.
	Root string `json:"-"`
}

// OpsPath joins parts under <root>/ops.
func (c *Config) OpsPath(parts ...string) string {
	return filepath.Join(append([]string{c.Root, "ops"}, parts...)...)
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{TmuxPrefix: "life"}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, p := range []*string{&c.CertFile, &c.KeyFile, &c.TokenFile, &c.ClaudeProjectsDir, &c.JobsDir, &c.ProjectsRoot, &c.ClaudeBin, &c.DBPath, &c.SchedulePath, &c.BlobDir, &c.APNsKeyFile} {
		*p = expand(*p)
	}
	if abs, err := filepath.Abs(path); err == nil {
		c.Root = filepath.Dir(filepath.Dir(abs))
	}
	if c.OwnerName == "" {
		c.OwnerName = "the owner"
	}
	for i := range c.Projects {
		c.Projects[i].Dir = expand(c.Projects[i].Dir)
	}
	if c.ClaudeBin == "" {
		return nil, fmt.Errorf("claude_bin required (run `which claude`)")
	}
	if _, err := os.Stat(c.ClaudeBin); err != nil {
		return nil, fmt.Errorf("claude_bin: %w", err)
	}
	if c.DBPath == "" {
		return nil, fmt.Errorf("db_path required")
	}
	if c.ListenAddr == "" {
		return nil, fmt.Errorf("listen_addr required")
	}
	if strings.HasPrefix(c.ListenAddr, "0.0.0.0") || strings.HasPrefix(c.ListenAddr, ":") {
		return nil, fmt.Errorf("listen_addr %q would bind all interfaces; refuse", c.ListenAddr)
	}
	return c, nil
}

// AllProjects merges the explicit list with subdirectories of ProjectsRoot.
func (c *Config) AllProjects() []Project {
	seen := map[string]bool{}
	var out []Project
	for _, p := range c.Projects {
		seen[p.Name] = true
		out = append(out, p)
	}
	if c.ProjectsRoot != "" {
		ents, _ := os.ReadDir(c.ProjectsRoot)
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !seen[e.Name()] {
				out = append(out, Project{Name: e.Name(), Dir: filepath.Join(c.ProjectsRoot, e.Name())})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Config) Project(name string) (Project, bool) {
	for _, p := range c.AllProjects() {
		if p.Name == name {
			return p, true
		}
	}
	return Project{}, false
}
