package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// A write the console sends again is answered once. The console's api() retries a request
// whose fetch() rejected with a TypeError, which is a hub restart's refused
// connection, but also a link that dropped after the hub had the request: a
// chat message or a Send then landed twice. The phone replays only GET, HEAD
// and PUT for that reason (HubClient.dataRetryingRestart); fetch() cannot tell
// the two failures apart, so the console names each write with a random
// X-Life-Request instead, and the hub runs a name once and hands any repeat
// the first answer, waiting for it if the first is still running. A request
// without the header is untouched.
const (
	replayHeader = "X-Life-Request"
	replayKeep   = 2 * time.Minute
)

type replayed struct {
	done   chan struct{}
	at     time.Time
	code   int
	header http.Header
	body   []byte
}

type replayCache struct {
	mu   sync.Mutex
	seen map[string]*replayed
}

var replays = &replayCache{seen: map[string]*replayed{}}

// replayKey: the caller's name for the write, tied to its credentials, so a
// name seen from one caller is never an answer for another.
func replayKey(r *http.Request) string {
	id := r.Header.Get(replayHeader)
	if id == "" || len(id) > 128 || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return ""
	}
	h := sha256.New()
	for _, part := range []string{r.Header.Get("Authorization"), r.Header.Get("Cookie"), r.Method, r.URL.Path, id} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// serveOnce runs h for the first request under a name and replays its answer
// to every repeat within replayKeep.
func (c *replayCache) serveOnce(w http.ResponseWriter, r *http.Request, h http.Handler) {
	key := replayKey(r)
	if key == "" {
		h.ServeHTTP(w, r)
		return
	}
	now := time.Now()
	c.mu.Lock()
	for k, e := range c.seen {
		if now.Sub(e.at) > replayKeep {
			select {
			case <-e.done:
				delete(c.seen, k)
			default:
			}
		}
	}
	e, again := c.seen[key]
	if !again {
		e = &replayed{done: make(chan struct{}), at: now, code: http.StatusOK}
		c.seen[key] = e
	}
	c.mu.Unlock()

	if again {
		select {
		case <-e.done:
		case <-r.Context().Done():
			return
		}
		for k, v := range e.header {
			w.Header()[k] = v
		}
		w.Header().Set("X-Life-Replayed", "1")
		w.WriteHeader(e.code)
		_, _ = w.Write(e.body)
		return
	}
	rec := &recordWriter{ResponseWriter: w, rec: e}
	defer func() {
		e.header = w.Header().Clone()
		e.body = rec.buf.Bytes()
		close(e.done)
	}()
	h.ServeHTTP(rec, r)
}

// recordWriter passes the answer through and keeps a copy for a repeat.
type recordWriter struct {
	http.ResponseWriter
	rec   *replayed
	buf   bytes.Buffer
	wrote bool
}

func (w *recordWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		w.rec.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *recordWriter) Write(b []byte) (int, error) {
	w.wrote = true
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}
