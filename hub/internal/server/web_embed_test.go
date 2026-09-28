package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Every asset index.html asks for must actually be in the binary.
//
// The bug this exists to prevent: composer.js was added to web/
// and to index.html, but the //go:embed line was a hand-kept file list, so
// /static/composer.js 404'd in the served console. Nothing failed to build,
// nothing failed to test — the page just went blank with "composerReset is
// not defined" the moment a recommendation was accepted.
var staticRef = regexp.MustCompile(`(?:src|href)="/static/([^"]+)"`)

func TestIndexAssetsAreEmbedded(t *testing.T) {
	index, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	refs := staticRef.FindAllStringSubmatch(string(index), -1)
	if len(refs) < 5 {
		t.Fatalf("found %d /static/ references in index.html, expected the console's scripts", len(refs))
	}
	for _, m := range refs {
		if _, err := webFS.ReadFile("web/" + m[1]); err != nil {
			t.Errorf("index.html loads /static/%s but it is not embedded (widen the //go:embed line in server.go): %v", m[1], err)
		}
	}
}

// The console is a hash router, so a tab left open runs the JS it was
// born with until the document itself is reloaded — which fragment navigation
// never does. `X-Hub-Build` is how the page finds out it has been rebuilt
// underneath itself (server.go consoleBuild, app.js seenBuild). It has to be
// on EVERY response, because the only request a console makes for twenty
// minutes at a time may be a badge poll that comes back 304 from the ETag
// layer.
func TestEveryResponseStampsTheConsoleBuild(t *testing.T) {
	s := newTest(t)
	if len(consoleBuild) != 12 {
		t.Fatalf("consoleBuild = %q, want a 12-char hash", consoleBuild)
	}
	for _, path := range []string{"/api/v1/board?surface=web", "/static/app.js", "/api/v1/nope"} {
		w := s.do(t, "GET", path, nil)
		if got := w.Header().Get("X-Hub-Build"); got != consoleBuild {
			t.Errorf("%s (%d): X-Hub-Build = %q, want %q", path, w.Code, got, consoleBuild)
		}
	}
	// A 304 keeps it too: that is the response a long-lived tab actually gets.
	w := s.do(t, "GET", "/static/app.js", nil)
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on a static file; the 304 path below proves nothing")
	}
	r := httptest.NewRequest("GET", "/static/app.js", nil)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != 304 {
		t.Fatalf("If-None-Match did not give a 304: %d", rec.Code)
	}
	if got := rec.Header().Get("X-Hub-Build"); got != consoleBuild {
		t.Fatalf("304 dropped X-Hub-Build: %q", got)
	}
}

// The stamp must be a hash of the FILES, not of the build. Restarting the hub
// with nothing changed must not reload a page someone is reading.
func TestConsoleBuildIsContentAddressed(t *testing.T) {
	h := sha256.New()
	_ = fs.WalkDir(webFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := webFS.ReadFile(p)
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	if want := hex.EncodeToString(h.Sum(nil))[:12]; want != consoleBuild {
		t.Fatalf("consoleBuild = %q, recomputed %q", consoleBuild, want)
	}
}

// web/test/ is the Node suite (ops/webcheck.sh); it has no business in the
// binary, which is the only reason the embed line is not a bare `web`.
func TestNodeTestsAreNotEmbedded(t *testing.T) {
	err := fs.WalkDir(webFS, "web", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(path, "web/test/") {
			t.Errorf("%s is embedded in the binary; the Node tests must stay out", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk webFS: %v", err)
	}
}
