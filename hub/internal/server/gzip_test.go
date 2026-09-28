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
