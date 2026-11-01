// Package spend parses Claude Code's local JSONL transcripts
// (~/.claude/projects/<encoded-cwd>/<session>.jsonl) into per-message usage.
// There is no billing API for consumer plans; this is the only source.
package spend

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

// Usage is one billed assistant message.
type Usage struct {
	SessionID    string
	Cwd          string
	Model        string
	TS           time.Time
	Input        int
	Output       int
	CacheRead    int
	CacheWrite5m int
	CacheWrite1h int
}

type record struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			Input         int `json:"input_tokens"`
			Output        int `json:"output_tokens"`
			CacheRead     int `json:"cache_read_input_tokens"`
			CacheWrite    int `json:"cache_creation_input_tokens"`
			CacheCreation struct {
				H1 int `json:"ephemeral_1h_input_tokens"`
				M5 int `json:"ephemeral_5m_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

// maxLine: a transcript line longer than this is skipped, never parsed. A
// billed assistant record is a few KB; the long lines are tool results (a
// pasted file, a screenshot's base64), which carry no usage.
var maxLine = 64 << 20

// readLine is the next line of r without its newline; long reports a line
// over maxLine, read past and dropped. err is io.EOF after the last line.
func readLine(r *bufio.Reader) (line []byte, long bool, err error) {
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, false, err
		}
		if !long {
			line = append(line, chunk...)
			if len(line) > maxLine {
				line, long = nil, true
			}
		}
		if !isPrefix {
			return line, long, nil
		}
	}
}

// ParseFile returns the deduplicated usages in one transcript. Claude Code
// writes one record per content block, all carrying the same message id and
// usage, so we keep the last record per (message id, request id) — same
// rule ccusage uses.
//
// A line over maxLine is skipped and the rest of the file still read. With a
// bufio.Scanner it ended the scan with ErrTooLong, and Cache.refresh drops a
// file whose parse errs, so one oversized tool result took that session's
// whole spend off the board, the lines before it included.
func ParseFile(path string) ([]Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	byKey := map[string]Usage{}
	var order []string
	rd := bufio.NewReaderSize(f, 1<<20)
	skipped := 0
	for {
		line, long, err := readLine(rd)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if long {
			skipped++
			continue
		}
		if !strings.Contains(string(line), `"type":"assistant"`) {
			continue
		}
		var r record
		if json.Unmarshal(line, &r) != nil || r.Type != "assistant" || r.Message.Model == "" {
			continue
		}
		if r.Message.Model == "<synthetic>" {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		u := r.Message.Usage
		w5, w1 := u.CacheCreation.M5, u.CacheCreation.H1
		if w5+w1 == 0 {
			w5 = u.CacheWrite
		}
		key := r.Message.ID + "/" + r.RequestID
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = Usage{
			SessionID: r.SessionID, Cwd: r.Cwd, Model: r.Message.Model, TS: ts,
			Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
			CacheWrite5m: w5, CacheWrite1h: w1,
		}
	}
	if skipped > 0 {
		log.Printf("spend: %s: %d line(s) over %d bytes skipped", path, skipped, maxLine)
	}
	out := make([]Usage, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out, nil
}

// ParseAll walks the projects dir once. The hub keeps a Cache instead (one
// parse per transcript per size+mtime); this is the one-shot form for tests
// and tools.
func ParseAll(root string) ([]Usage, error) {
	return NewCache(root).refresh()
}
