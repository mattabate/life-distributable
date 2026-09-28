package spend

import (
	"encoding/gob"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache is ParseAll that remembers. ~/.claude/projects runs to thousands of
// transcripts and hundreds of MB and grows every turn; parsing all of it takes
// seconds, and doing it again on every Spend request made the page slow. Three
// rules fix it:
//
//   - a transcript is parsed once per (size, mtime). A refresh is a stat walk
//     plus the handful of files that changed since — the running sessions.
//   - a snapshot older than MaxAge is served as it is and refreshed BEHIND
//     the request, one refresh at a time. Only a hub that has never parsed
//     (or has never seen a transcript) waits for the walk.
//   - with Store set, the parsed tree is written to that file (gob) and read
//     back at the next start, so a restart costs a stat walk plus the files
//     that changed since — not the whole tree. The tree reaches GBs and
//     the hub restarts often (every session that ships hub code restarts
//     it); a cold walk takes tens of seconds, and the console's + New
//     session would wait on it.
type Cache struct {
	Root   string
	MaxAge time.Duration // 0 = defaultMaxAge
	Store  string        // the parsed tree, kept across restarts; "" = never written

	mu         sync.Mutex // guards the fields below
	files      map[string]cachedFile
	snap       []Usage // every usage under Root, oldest first; nil = never parsed
	snapAt     time.Time
	refreshing bool
	parses     int // transcripts parsed so far (tests read it)
	loaded     bool
	saving     bool
	savedAt    time.Time

	refreshMu sync.Mutex // one walk at a time
}

type cachedFile struct {
	size int64
	mod  time.Time
	us   []Usage
}

// storedFile / storedTree are cachedFile / files with exported names, which
// is what encoding/gob writes. Usage's own fields are all exported.
type storedFile struct {
	Size int64
	Mod  time.Time
	Us   []Usage
}

type storedTree struct {
	At    time.Time
	Files map[string]storedFile
}

const defaultMaxAge = 30 * time.Second

// saveEvery bounds how often a changed tree is rewritten to Store: the live
// sessions' transcripts change on every turn, and a 5 MB file every 30 s is
// not worth the disk. A restart re-parses at most a minute of changes.
const saveEvery = time.Minute

func NewCache(root string) *Cache { return &Cache{Root: root} }

func (c *Cache) maxAge() time.Duration {
	if c.MaxAge > 0 {
		return c.MaxAge
	}
	return defaultMaxAge
}

// Usages returns every usage under Root, sorted by time. The slice is shared:
// read it, never write to it.
func (c *Cache) Usages() ([]Usage, error) {
	c.mu.Lock()
	// The first call after a start: the tree as it was written at the last
	// save becomes the snapshot, dated then, so it is served at once and the
	// walk that catches up the changed files runs behind this request.
	if c.snap == nil && c.Store != "" && !c.loaded {
		c.loaded = true
		c.loadLocked()
	}
	// A snapshot of an empty tree is not worth keeping: a transcript written a
	// moment later must show up on the next call, not in 30 s.
	if c.snap != nil && len(c.files) > 0 {
		us := c.snap
		if time.Since(c.snapAt) >= c.maxAge() && !c.refreshing {
			c.refreshing = true
			go c.refresh()
		}
		c.mu.Unlock()
		return us, nil
	}
	c.mu.Unlock()
	return c.refresh()
}

// Prime parses once in the background so the first request after a restart
// does not pay for the walk.
func (c *Cache) Prime() { go c.Usages() }

// refresh walks Root, reparses what changed, and installs the new snapshot.
func (c *Cache) refresh() ([]Usage, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.mu.Lock()
	old := c.files
	c.mu.Unlock()

	next := make(map[string]cachedFile, len(old)+16)
	parsed := 0
	err := filepath.WalkDir(c.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if e, ok := old[p]; ok && e.size == info.Size() && e.mod.Equal(info.ModTime()) {
			next[p] = e
			return nil
		}
		us, err := ParseFile(p)
		if err != nil {
			return nil
		}
		next[p] = cachedFile{size: info.Size(), mod: info.ModTime(), us: us}
		parsed++
		return nil
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.parses += parsed
	c.refreshing = false
	changed := parsed > 0 || len(next) != len(old)
	if c.snap == nil || changed {
		c.snap = flattenFiles(next)
	}
	c.files = next
	c.snapAt = time.Now()
	// A changed tree goes to Store, behind the caller and at most once a
	// minute. `next` is never written to again — every refresh builds a new
	// map — so the writer needs no lock on it.
	if changed && c.Store != "" && !c.saving && time.Since(c.savedAt) >= saveEvery {
		c.saving = true
		go c.save(next, c.snapAt)
	}
	return c.snap, err
}

// flattenFiles is every file's usages in one slice, oldest first.
func flattenFiles(files map[string]cachedFile) []Usage {
	paths := make([]string, 0, len(files))
	n := 0
	for p, e := range files {
		paths = append(paths, p)
		n += len(e.us)
	}
	sort.Strings(paths) // a stable order in, a stable order out
	all := make([]Usage, 0, n)
	for _, p := range paths {
		all = append(all, files[p].us...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].TS.Before(all[j].TS) })
	return all
}

// loadLocked reads Store into files/snap/snapAt. Called with mu held. A file
// that is missing, unreadable or from another shape of Usage is ignored: the
// walk then starts from nothing, as before Store existed.
func (c *Cache) loadLocked() {
	f, err := os.Open(c.Store)
	if err != nil {
		return
	}
	defer f.Close()
	var t storedTree
	if err := gob.NewDecoder(f).Decode(&t); err != nil || len(t.Files) == 0 {
		if err != nil {
			log.Printf("spend: ignoring %s: %v", c.Store, err)
		}
		return
	}
	files := make(map[string]cachedFile, len(t.Files))
	for p, e := range t.Files {
		files[p] = cachedFile{size: e.Size, mod: e.Mod, us: e.Us}
	}
	c.files = files
	c.snap = flattenFiles(files)
	c.snapAt = t.At
	c.savedAt = t.At
	log.Printf("spend: %d usages in %d transcripts from %s (as of %s)", len(c.snap), len(files), filepath.Base(c.Store), t.At.Format("15:04:05"))
}

// save writes the tree to Store, whole file then rename, so a hub stopped
// mid-write leaves the previous file in place.
func (c *Cache) save(files map[string]cachedFile, at time.Time) {
	defer func() {
		c.mu.Lock()
		c.saving = false
		c.savedAt = at
		c.mu.Unlock()
	}()
	t := storedTree{At: at, Files: make(map[string]storedFile, len(files))}
	for p, e := range files {
		t.Files[p] = storedFile{Size: e.size, Mod: e.mod, Us: e.us}
	}
	if err := os.MkdirAll(filepath.Dir(c.Store), 0o755); err != nil {
		log.Printf("spend: %v", err)
		return
	}
	tmp := c.Store + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		log.Printf("spend: %v", err)
		return
	}
	if err := gob.NewEncoder(f).Encode(&t); err != nil {
		f.Close()
		os.Remove(tmp)
		log.Printf("spend: writing %s: %v", c.Store, err)
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		log.Printf("spend: %v", err)
		return
	}
	if err := os.Rename(tmp, c.Store); err != nil {
		os.Remove(tmp)
		log.Printf("spend: %v", err)
	}
}
