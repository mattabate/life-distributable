package threads

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// RunAs starts session processes as another macOS user (config session_user),
// so a session cannot write the owner's checkout, read ops/secrets or write
// the database. Empty User = sessions run as the hub's own user, as before.
//
// Normal Claude sign-in cannot save under sudo: that user's login keychain is
// locked. A `claude setup-token` token in a file in that user's home needs no
// keychain and no one logged in (sessionUserEnv).
type RunAs struct {
	User      string // the macOS user; sudoers lets the hub's user act as it
	ClaudeBin string // absolute; that user's own install
	TmuxBin   string // absolute: sudo's secure_path has no Homebrew
	// CloneRoot holds each session's own clone, th-<id>: that user cannot write
	// the main checkout, so it works in a clone and lands through the hub
	// (server/land.go).
	CloneRoot string
	// Repo is the main checkout (config Root): the project whose sessions get
	// a clone. A session of another project runs in its own directory.
	Repo string
}

// asSession runs one command as the session user, or as the hub's own.
func (m *Manager) asSession(name string, args ...string) ([]byte, error) {
	if m.RunAs.User == "" {
		return m.Run(name, args...)
	}
	return m.Run("/usr/bin/sudo", append([]string{"-n", "-H", "-u", m.RunAs.User, name}, args...)...)
}

// tmux runs one tmux command on the server that holds the session processes,
// which belongs to the session user when RunAs is on.
func (m *Manager) tmux(args ...string) ([]byte, error) {
	if m.RunAs.User == "" {
		return m.Run("tmux", args...)
	}
	return m.Run("/usr/bin/sudo", append([]string{"-n", "-H", "-u", m.RunAs.User, m.RunAs.TmuxBin}, args...)...)
}

// sessionClaudeBin is the CLI a session process runs.
func (m *Manager) sessionClaudeBin() string {
	if m.RunAs.User == "" {
		return m.ClaudeBin
	}
	return m.RunAs.ClaudeBin
}

// sessionUserEnv is what a session process needs that sudo clears: its login,
// the UTF-8 locale tmux wants, and Homebrew, its CLI and the main checkout's
// ops/bin (lifectl, which its grant names bare) on PATH. Expanded by the
// session user's own shell, so the hub never reads the token.
func (m *Manager) sessionUserEnv() string {
	if m.RunAs.User == "" {
		return ""
	}
	return ` CLAUDE_CODE_OAUTH_TOKEN="$(cat "$HOME/.claude-token")" LANG=en_US.UTF-8 PATH=` +
		shellQuote(filepath.Join(m.RunAs.Repo, "ops", "bin")) + `:/opt/homebrew/bin:"$HOME/.local/bin":"$PATH"`
}

// cloneLinks: git-ignored paths a clone reaches in the main checkout. data/
// (read through the hub; the session user cannot write it) and ops/hub.json,
// which lifectl and ops/hublib.py read. Never ops/secrets (that user cannot
// read it) and never ops/logs or ops/bin: a clone's logs and builds are its own.
var cloneLinks = []string{"data", "ops/hub.json"}

// sessionDir: the directory a thread's process runs in. As the session user,
// a session of the main checkout gets its own clone, caught up to main before
// each start. Any failure falls back to the shared checkout (logged): a
// session that runs, read-only, beats one that does not.
func (m *Manager) sessionDir(t Thread, dir string) string {
	if m.RunAs.User == "" || m.RunAs.CloneRoot == "" || filepath.Clean(dir) != filepath.Clean(m.RunAs.Repo) {
		return dir
	}
	wt := filepath.Join(m.RunAs.CloneRoot, "th-"+t.ID)
	if m.cloneExists(wt) {
		m.catchUp(t, wt)
		return wt
	}
	got := m.sessionClone(t, dir, wt)
	// A session that began in the shared tree has its transcript filed under
	// that directory: `--resume` from the clone would find nothing.
	if got == wt && t.ClaudeSessionID != "" && !m.transcriptUnder(wt, t.ClaudeSessionID) {
		m.carryTranscript(t, wt)
	}
	return got
}

// catchUp brings a clone's branch up to main before a process starts in it,
// when that is a fast-forward. A branch with unlanded commits, or edits the
// merge would overwrite, is left as it is.
func (m *Manager) catchUp(t Thread, wt string) {
	if _, err := m.asSession("git", "-C", wt, "fetch", "--quiet", "origin", "main"); err != nil {
		return
	}
	if out, err := m.asSession("git", "-C", wt, "merge", "--ff-only", "--quiet", "origin/main"); err != nil {
		log.Printf("thread %s: clone not caught up to main: %s", t.ID, strings.TrimSpace(string(out)))
	}
}

// sessionClone: the session user's own clone of the main checkout, on branch
// th-<id>, which it lands through the hub (ops/land.sh, POST /api/v1/land).
func (m *Manager) sessionClone(t Thread, dir, wt string) string {
	if out, err := m.asSession("git", "clone", "--quiet", "--no-hardlinks", dir, wt); err != nil {
		log.Printf("thread %s: clone: %v %s; running in the shared tree", t.ID, err, strings.TrimSpace(string(out)))
		return dir
	}
	if _, err := m.asSession("git", "-C", wt, "checkout", "--quiet", "th-"+t.ID); err != nil {
		if out, err := m.asSession("git", "-C", wt, "checkout", "--quiet", "-b", "th-"+t.ID, "origin/main"); err != nil {
			log.Printf("thread %s: clone branch: %v %s; running in the shared tree", t.ID, err, strings.TrimSpace(string(out)))
			return dir
		}
	}
	for _, p := range cloneLinks {
		src := filepath.Join(dir, p)
		if !fileExists(src) {
			continue
		}
		if out, err := m.asSession("ln", "-s", src, filepath.Join(wt, p)); err != nil {
			log.Printf("thread %s: clone link %s: %v %s", t.ID, p, err, strings.TrimSpace(string(out)))
		}
	}
	if out, err := m.asSession("mkdir", "-p", filepath.Join(wt, "ops", "logs")); err != nil {
		log.Printf("thread %s: clone ops/logs: %v %s", t.ID, err, strings.TrimSpace(string(out)))
	}
	return wt
}

// cliProjectSlug: the CLI's projects/ folder name for a cwd: every character
// that is not a letter or digit becomes '-'.
func cliProjectSlug(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// carryTranscript copies a session's transcript from wherever the CLI filed
// it to the folder for its clone, so `--resume` there finds the conversation.
func (m *Manager) carryTranscript(t Thread, wt string) {
	if m.Transcripts == "" {
		return
	}
	src, _ := filepath.Glob(filepath.Join(m.Transcripts, "*", t.ClaudeSessionID+".jsonl"))
	if len(src) == 0 {
		return
	}
	dst := filepath.Join(m.Transcripts, cliProjectSlug(wt))
	m.asSession("mkdir", "-p", dst)
	if out, err := m.asSession("cp", src[0], filepath.Join(dst, t.ClaudeSessionID+".jsonl")); err != nil {
		log.Printf("thread %s: transcript to clone: %v %s", t.ID, err, strings.TrimSpace(string(out)))
	}
}

// transcriptUnder: the CLI filed session id's transcript under cwd.
func (m *Manager) transcriptUnder(cwd, id string) bool {
	return m.Transcripts != "" && fileExists(filepath.Join(m.Transcripts, cliProjectSlug(cwd), id+".jsonl"))
}

// cloneExists asks as the session user, whose home the hub's user may not be
// able to list.
func (m *Manager) cloneExists(p string) bool {
	if _, err := os.Lstat(p); err == nil {
		return true
	}
	_, err := m.asSession("test", "-d", p)
	return err == nil
}
