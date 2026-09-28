package threads

// Token totals per chat: the cost of every chat at the present time. Live
// counting happens in runner.go; this file is the one-off catch-up for chats
// that ran before the columns existed — without it the cost screen would show
// a blank against every session that already exists.

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"
)

// backfillTokens sums each finished run's stream-json output into its thread's
// token totals, once. It is skipped the moment any thread has a total (that
// is, after it has run once, or after any new turn has settled), so it never
// double-counts. The parse runs in the background: the run logs are hundreds
// of megabytes and the hub should not wait on them to start serving.
func (m *Manager) backfillTokens() {
	var have int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM threads WHERE tok_in+tok_out+tok_cache_read+tok_cache_write > 0`).Scan(&have); err != nil || have > 0 {
		return
	}
	rows, err := m.db.Query(`SELECT thread_id, out_file FROM thread_runs WHERE out_file != ''`)
	if err != nil {
		return
	}
	type job struct{ thread, out string }
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.thread, &j.out) == nil {
			jobs = append(jobs, j)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("threads: token backfill: %v", err)
	}
	rows.Close()
	if len(jobs) == 0 {
		return
	}
	go func() {
		start := time.Now()
		totals, files := map[string]*Tokens{}, 0
		for _, j := range jobs {
			t, ok := tokensInRunLog(j.out)
			if !ok {
				continue // log rotated away or never written; that thread stays short
			}
			files++
			cur := totals[j.thread]
			if cur == nil {
				cur = &Tokens{}
				totals[j.thread] = cur
			}
			cur.In += t.In
			cur.Out += t.Out
			cur.CacheRead += t.CacheRead
			cur.CacheWrite += t.CacheWrite
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		// Re-check the guard: a turn may have settled while we were parsing,
		// and its tokens are already on the thread.
		var have int
		if err := m.db.QueryRow(`SELECT COUNT(*) FROM threads WHERE tok_in+tok_out+tok_cache_read+tok_cache_write > 0`).Scan(&have); err != nil || have > 0 {
			return
		}
		for id, t := range totals {
			m.db.Exec(`UPDATE threads SET tok_in=?, tok_out=?, tok_cache_read=?, tok_cache_write=? WHERE id=?`, t.In, t.Out, t.CacheRead, t.CacheWrite, id)
		}
		log.Printf("threads: back-filled tokens for %d chat(s) from %d/%d run log(s) in %s", len(totals), files, len(jobs), time.Since(start).Round(time.Second))
	}()
}

// tokensInRunLog adds up the usage on every result line of one run's output —
// the same figure finishTurn records live, since each result line reports the
// usage of the turn it closes.
func tokensInRunLog(path string) (Tokens, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Tokens{}, false
	}
	defer f.Close()
	var out Tokens
	sc := bufio.NewScanner(f)
	// Run logs carry whole tool results on one line; the default 64 KB limit
	// would stop the scan at the first big one and undercount the rest.
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, `"total_cost_usd"`) {
			continue
		}
		var env resultEnvelope
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		t := env.tokens()
		out.In += t.In
		out.Out += t.Out
		out.CacheRead += t.CacheRead
		out.CacheWrite += t.CacheWrite
	}
	out.sum()
	return out, true
}
