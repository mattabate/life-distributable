// Package workspace is a role's own files: a standing session's memory at a
// larger scale than the goal notes (team mode, threads.Persona).
//
// One folder per role under data/people/<role>/ (never committed, backed up
// with the rest of data/). Every session reads any of them; only the session
// that plays the role, or the owner from a shell, writes to one. A write
// never loses what was there: the previous version of an overwritten file is
// kept under .versions/. Files are text (markdown by habit); pictures stay
// blobs.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MaxBytes: the largest file a workspace takes (a working table, a long
// study). Anything bigger belongs in a blob or the database.
const MaxBytes = 16 << 20

type Store struct {
	// Dir: data/people.
	Dir string
	// Role resolves a session id to its persona key, "" for none.
	Role func(thread string) string
	// Known says whether a persona key exists.
	Known func(key string) bool
}

// File: one entry of a listing.
type File struct {
	Persona   string `json:"persona"`
	Name      string `json:"name"`
	Bytes     int64  `json:"bytes"`
	UpdatedAt string `json:"updated_at"`
	// Head: the file's first non-empty line, ≤100 chars — what a listing
	// shows so a session can tell what is there without reading each file.
	Head string `json:"head,omitempty"`
}

var ErrForbidden = errors.New("only the session that plays this role, or the owner, writes to its workspace")

// Resolve turns a persona key or a session id into a persona key; "" when
// neither names a role.
func (s *Store) Resolve(key string) string {
	if s.Known != nil && s.Known(key) {
		return key
	}
	if s.Role != nil {
		return s.Role(key)
	}
	return ""
}

// List: every file of one persona, or of every persona when key is "",
// sorted by persona then name. A persona with no folder yet lists nothing.
func (s *Store) List(key string) ([]File, error) {
	personas := []string{key}
	if key == "" {
		ents, err := os.ReadDir(s.Dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		personas = personas[:0]
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				personas = append(personas, e.Name())
			}
		}
	}
	out := []File{}
	for _, p := range personas {
		root := filepath.Join(s.Dir, p)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if strings.HasPrefix(d.Name(), ".") && path != root {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			out = append(out, File{Persona: p, Name: filepath.ToSlash(rel), Bytes: info.Size(),
				UpdatedAt: info.ModTime().UTC().Format(time.RFC3339), Head: head(path)})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Persona != out[j].Persona {
			return out[i].Persona < out[j].Persona
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Count: how many files a persona holds (0 for none, or no folder).
func (s *Store) Count(key string) int {
	fs, _ := s.List(key)
	return len(fs)
}

// Read: one file's bytes.
func (s *Store) Read(key, name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if !s.known(key) {
		return nil, fmt.Errorf("no such persona %q", key)
	}
	return os.ReadFile(filepath.Join(s.Dir, key, filepath.FromSlash(name)))
}

// Write: store b as the persona's file `name`, by `by` (owner, or
// claude:thread:<id> — the session must play the role). An existing file is
// first copied to .versions/<name>.<stamp>, so nothing is destroyed.
func (s *Store) Write(key, name string, b []byte, by string) (File, error) {
	if err := checkName(name); err != nil {
		return File{}, err
	}
	if !s.known(key) {
		return File{}, fmt.Errorf("no such persona %q", key)
	}
	if len(b) > MaxBytes {
		return File{}, fmt.Errorf("file is %d bytes; the cap is %d", len(b), MaxBytes)
	}
	if !s.mayWrite(key, by) {
		return File{}, ErrForbidden
	}
	path := filepath.Join(s.Dir, key, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return File{}, err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) != string(b) {
		v := filepath.Join(s.Dir, key, ".versions", filepath.FromSlash(name)+"."+time.Now().UTC().Format("20060102T150405"))
		if err := os.MkdirAll(filepath.Dir(v), 0o755); err != nil {
			return File{}, err
		}
		if err := os.WriteFile(v, old, 0o644); err != nil {
			return File{}, err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".put-*")
	if err != nil {
		return File{}, err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return File{}, err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return File{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	return File{Persona: key, Name: name, Bytes: info.Size(), UpdatedAt: info.ModTime().UTC().Format(time.RFC3339), Head: head(path)}, nil
}

func (s *Store) known(key string) bool {
	if s.Known == nil {
		return key != "" && safeSegment(key) && !strings.HasPrefix(key, ".")
	}
	return s.Known(key)
}

// mayWrite: the owner from a shell, or the session that plays the role.
func (s *Store) mayWrite(key, by string) bool {
	if by == "" || by == "owner" {
		return true
	}
	id, ok := strings.CutPrefix(by, "claude:thread:")
	if !ok || s.Role == nil {
		return false
	}
	return s.Role(id) == key
}

// checkName: a relative path of 1-4 segments, each letters, digits, `.`,
// `_` or `-`, none starting with a dot (.versions is the hub's), ≤120 chars.
func checkName(name string) error {
	if name == "" || len(name) > 120 {
		return errors.New("name: 1-120 characters")
	}
	segs := strings.Split(name, "/")
	if len(segs) > 4 {
		return errors.New("name: at most 4 path segments")
	}
	for _, seg := range segs {
		if seg == "" || strings.HasPrefix(seg, ".") || !safeSegment(seg) {
			return fmt.Errorf("name: segment %q must be letters, digits, '.', '_' or '-', not starting with a dot", seg)
		}
	}
	return nil
}

func safeSegment(seg string) bool {
	for _, c := range seg {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return seg != ""
}

// head: the first non-empty line of a file, markdown heading marks and
// leading spaces dropped, cut to 100 runes.
func head(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > 100 {
			line = string(r[:100]) + "…"
		}
		return line
	}
	return ""
}
