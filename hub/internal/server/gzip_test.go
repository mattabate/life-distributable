package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve runs one handler through the same wrapper ServeHTTP applies.
func serve(h http.HandlerFunc, accept string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/x", nil)
	if accept != "" {
		r.Header.Set("Accept-Encoding", accept)
	}
	w := httptest.NewRecorder()
	gw, done := maybeGzip(w, r)
	h(gw, r)
	done()
	return w
}

// A big JSON body goes out gzipped and decodes to exactly what the handler
// wrote; a small one is left alone (the header would cost more than it saves);
// a client that never asked for gzip still gets plain bytes; and an image is
// never re-compressed.
func TestGzipResponses(t *testing.T) {
	events := make([]map[string]string, 60)
	for i := range events {
		events[i] = map[string]string{"kind": "tool_use", "title": "Bash", "body": strings.Repeat("output ", 130)}
	}
	big := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, events) }

	w := serve(big, "gzip, deflate")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("big body not compressed (%d bytes)", w.Body.Len())
	}
	if w.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length left over from the uncompressed body")
	}
	if w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary = %q", w.Header().Get("Vary"))
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 60 || got[7]["title"] != "Bash" {
		t.Fatalf("body changed through gzip: %d rows", len(got))
	}
	if compressed, plain := w.Body.Len(), len(raw); compressed*4 > plain {
		t.Fatalf("gzip barely helped: %d -> %d", plain, compressed)
	}

	// Small reply: not worth compressing, and it keeps its exact bytes.
	small := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"ok": "yes"}) }
	if w := serve(small, "gzip"); w.Header().Get("Content-Encoding") != "" || !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("tiny body compressed: %q", w.Body.String())
	}
	// Client that did not ask for it gets plain JSON.
	if w := serve(big, ""); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("compressed for a client that never advertised gzip")
	}
	// Already-compressed bytes (a photo from /blobs) pass through untouched.
	jpeg := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte(strings.Repeat("\xff\xd8\xff\xe0", 900)))
	}
	if w := serve(jpeg, "gzip"); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("re-compressed a jpeg")
	}
	// A non-200 through the same wrapper keeps its status and its body.
	fail := func(w http.ResponseWriter, r *http.Request) { jsonErr(w, 404, "no such thread") }
	if w := serve(fail, "gzip"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "no such thread") {
		t.Fatalf("status/body lost through the gzip writer: %d %q", w.Code, w.Body.String())
	}
	// A handler that writes nothing at all still sends its status once.
	empty := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
	if w := serve(empty, "gzip"); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("204 = %d, %d bytes", w.Code, w.Body.Len())
	}
}

// A ranged read of a static file answers 206 with the plain slice: gzipping
// it would leave Content-Range describing bytes that are not on the wire (#176).
func TestGzipLeavesRangedResponsesAlone(t *testing.T) {
	s := newTest(t)
	r := httptest.NewRequest("GET", "/static/app.js", nil)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Accept-Encoding", "gzip")
	r.Header.Set("Range", "bytes=0-1999")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("ranged GET = %d, want 206", w.Code)
	}
	if ce := w.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("206 went out with Content-Encoding %q", ce)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Range"), "bytes 0-1999/") || w.Body.Len() != 2000 {
		t.Fatalf("Content-Range %q with a %d-byte body", w.Header().Get("Content-Range"), w.Body.Len())
	}
	// The same file unranged is still gzipped, so the check above is the status, not the type.
	r.Header.Del("Range")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("full GET = %d, Content-Encoding %q", w.Code, w.Header().Get("Content-Encoding"))
	}
}

// If-None-Match matches our tag as `*`, inside a comma list, or weakened by a
// proxy; a different tag still gets the full body.
func TestIfNoneMatchForms(t *testing.T) {
	tag := `"abc123"`
	for _, h := range []string{tag, "*", `"zzz", "abc123"`, `W/"abc123"`, `"x" ,W/"abc123"`} {
		if !noneMatch(h, tag) {
			t.Errorf("If-None-Match %q did not match %s", h, tag)
		}
	}
	for _, h := range []string{"", `"abc"`, `"abc1234"`, `W/"zzz"`} {
		if noneMatch(h, tag) {
			t.Errorf("If-None-Match %q matched %s", h, tag)
		}
	}
	body := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"ok": "yes"}) }
	r := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	cw, done := maybeETag(w, r)
	body(cw, r)
	done()
	r.Header.Set("If-None-Match", `"other", W/`+w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	cw, done = maybeETag(w, r)
	body(cw, r)
	done()
	if w.Code != http.StatusNotModified {
		t.Fatalf("weak tag in a list = %d, want 304", w.Code)
	}
}
