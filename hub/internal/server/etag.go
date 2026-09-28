package server

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"net"
	"net/http"
	"strings"
)

// condWriter gives every JSON GET an ETag and answers 304 Not Modified when
// the client already holds that exact body (speed phase of the 2026-08-26
// review). The phone polls the same handful of reads over and over — the
// thread list, the board, a chat's messages and asks — and away from home,
// over a relayed Tailscale link, each unchanged answer was still tens of KB
// gzipped and a full decode on the device. With this, an unchanged poll is a
// ~200-byte round trip and no decode at all; the hub still does its query
// (that was never the slow part: 2–30 ms).
//
// The tag is a hash of the body, so it needs no bookkeeping and is right by
// construction: two bodies match iff their bytes do. Only 200 JSON responses
// get one; anything else — errors, blobs, the console's pages, a handler
// that Flushes or Hijacks — passes straight through untouched.
type condWriter struct {
	http.ResponseWriter
	r       *http.Request
	code    int
	buf     bytes.Buffer
	direct  bool // passing through: not JSON, or the handler streams
	started bool // first Write seen (Content-Type is known by then)
}

func (c *condWriter) WriteHeader(code int) {
	if c.direct {
		c.ResponseWriter.WriteHeader(code)
		return
	}
	c.code = code
}

func (c *condWriter) Write(p []byte) (int, error) {
	if !c.started && !c.direct {
		c.started = true
		if c.code != http.StatusOK || !strings.HasPrefix(c.Header().Get("Content-Type"), "application/json") {
			c.passThrough()
		}
	}
	if c.direct {
		return c.ResponseWriter.Write(p)
	}
	return c.buf.Write(p)
}

// passThrough gives up on the tag: writes what is buffered and hands every
// later byte straight down.
func (c *condWriter) passThrough() {
	if c.direct {
		return
	}
	c.direct = true
	c.ResponseWriter.WriteHeader(c.code)
	if c.buf.Len() > 0 {
		c.ResponseWriter.Write(c.buf.Bytes())
		c.buf.Reset()
	}
}

func (c *condWriter) Flush() {
	c.passThrough()
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *condWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c.direct = true
	if h, ok := c.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("etag: underlying writer cannot hijack")
}

// finish writes the buffered response — as a 304 when the client's
// If-None-Match is the tag of this very body. Always called once by ServeHTTP.
func (c *condWriter) finish() {
	if c.direct {
		return
	}
	if c.code == http.StatusOK && c.buf.Len() > 0 {
		h := fnv.New64a()
		h.Write(c.buf.Bytes())
		tag := `"` + hex.EncodeToString(h.Sum(nil)) + `"`
		c.Header().Set("ETag", tag)
		if c.r.Header.Get("If-None-Match") == tag {
			c.Header().Del("Content-Type")
			c.ResponseWriter.WriteHeader(http.StatusNotModified)
			return
		}
	}
	c.ResponseWriter.WriteHeader(c.code)
	if c.buf.Len() > 0 {
		c.ResponseWriter.Write(c.buf.Bytes())
	}
}

// maybeETag wraps w for GET requests; other methods are handed through and
// done() is a no-op.
func maybeETag(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	if r.Method != http.MethodGet {
		return w, func() {}
	}
	c := &condWriter{ResponseWriter: w, r: r, code: http.StatusOK}
	return c, c.finish
}
