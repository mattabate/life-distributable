// Package progress: what an approved action is doing while it runs, in
// memory only, so the session that proposed it can show it. The runner
// (actions.run) opens an entry for the action, its executor reports into it
// (a shell's newest output line, or a count from an executor that has one),
// and the session payload reads it back (threads: Thread.RunningActions). A
// leaf package, so any executor can report without importing actions.
package progress

import (
	"context"
	"strings"
	"sync"
)

// Live is one running action's newest step.
type Live struct {
	Doing       string
	Done, Total int
}

type key struct{}

var (
	mu   sync.Mutex
	live = map[string]Live{}
)

// Start opens an entry for the action; the returned context carries it.
func Start(ctx context.Context, id string) context.Context {
	mu.Lock()
	live[id] = Live{}
	mu.Unlock()
	return context.WithValue(ctx, key{}, id)
}

// ID is the action ctx carries, "" outside one.
func ID(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}

// End drops the action's entry.
func End(id string) {
	mu.Lock()
	delete(live, id)
	mu.Unlock()
}

// Report sets the newest step of the action ctx carries; a no-op outside one.
func Report(ctx context.Context, doing string, done, total int) {
	id := ID(ctx)
	if id == "" {
		return
	}
	if r := []rune(doing); len(r) > 160 {
		doing = string(r[:159]) + "…"
	}
	mu.Lock()
	if _, ok := live[id]; ok {
		live[id] = Live{Doing: doing, Done: done, Total: total}
	}
	mu.Unlock()
}

// Get is the action's newest step, ok = it is running in this hub.
func Get(id string) (Live, bool) {
	mu.Lock()
	defer mu.Unlock()
	l, ok := live[id]
	return l, ok
}

// Lines is a writer that reports its newest non-blank line (a carriage-return
// redraw kept as its last frame) as the action's step.
func Lines(ctx context.Context) *LineWriter { return &LineWriter{ctx: ctx} }

type LineWriter struct {
	ctx  context.Context
	part string
}

func (w *LineWriter) Write(b []byte) (int, error) {
	s := w.part + string(b)
	if len(s) > 4096 {
		s = s[len(s)-4096:]
	}
	lines := strings.Split(s, "\n")
	w.part = lines[len(lines)-1]
	for i := len(lines) - 2; i >= 0; i-- {
		l := lines[i]
		if j := strings.LastIndex(strings.TrimRight(l, "\r"), "\r"); j >= 0 {
			l = l[j+1:]
		}
		if l = strings.TrimSpace(l); l != "" {
			Report(w.ctx, l, 0, 0)
			break
		}
	}
	return len(b), nil
}
