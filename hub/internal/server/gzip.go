package server

import (
	"bufio"
	"compress/gzip"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Every response left here uncompressed. That is fine for a 2 KB card list and
// bad for a session: opening one chat pulls its whole event stream, which is
// 340 KB of JSON on a middling thread and ~940 KB on the app-quality one, and
// the phone re-pulls it on every 3s poll. JSON of this shape gzips ~8x, so the
// chat that took seconds to appear now moves tens of KB.
//
// Applied at the single funnel in ServeHTTP, so the console gets it too.
// Bodies that are already compressed (images from /blobs) are left alone, and
// so is anything under gzipMin — the header costs more than it saves.
const gzipMin = 1400

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

// compressible reports whether a Content-Type is worth gzipping. Unknown types
// are left alone: an unset Content-Type on this server means a file body.
func compressible(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch {
	case strings.HasPrefix(ct, "text/"):
		return true
	case ct == "application/json", ct == "application/javascript", ct == "image/svg+xml":
		return true
	}
	return false
}

// gzipWriter buffers the first gzipMin bytes before deciding, so small replies
// (and the 204s) go out untouched and Content-Length stays correct for them.
type gzipWriter struct {
	http.ResponseWriter
	code    int
	buf     []byte
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) WriteHeader(code int) { g.code = code } // deferred until decide()

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.decided {
		g.buf = append(g.buf, p...)
		if len(g.buf) < gzipMin {
			return len(p), nil
		}
		if err := g.decide(true); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	if g.gz != nil {
		return g.gz.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

// decide writes the status line and flushes whatever is buffered, gzipping
// from here on when the body is big enough and of a compressible type.
func (g *gzipWriter) decide(big bool) error {
	g.decided = true
	if big && compressible(g.Header().Get("Content-Type")) {
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Add("Vary", "Accept-Encoding")
		g.Header().Del("Content-Length") // no longer the byte count on the wire
		g.gz = gzipPool.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(g.code)
	if len(g.buf) == 0 {
		return nil
	}
	var err error
	if g.gz != nil {
		_, err = g.gz.Write(g.buf)
	} else {
		_, err = g.ResponseWriter.Write(g.buf)
	}
	g.buf = nil
	return err
}

// Flush lets a handler stream (SSE, long polls): the first Flush decides
// with whatever is buffered, so headers go out, and pushes the gzip frame
// and the underlying writer. Without this http.Flusher would be hidden by
// the wrapper and a streaming handler would silently buffer forever.
func (g *gzipWriter) Flush() {
	if !g.decided {
		_ = g.decide(len(g.buf) >= gzipMin)
	}
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hands the connection through untouched (websockets); nothing is
// gzipped after that.
func (g *gzipWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := g.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("gzip: underlying writer cannot hijack")
	}
	g.decided = true
	return h.Hijack()
}

// Close flushes a short response (never big enough to decide) and finishes the
// gzip stream. Always called, exactly once, by ServeHTTP.
func (g *gzipWriter) Close() {
	if !g.decided {
		_ = g.decide(false)
	}
	if g.gz != nil {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}

// maybeGzip wraps w when the client advertises gzip; otherwise the response
// writer is handed through untouched and done() is a no-op.
func maybeGzip(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip") {
		return w, func() {}
	}
	g := &gzipWriter{ResponseWriter: w, code: http.StatusOK}
	return g, g.Close
}
