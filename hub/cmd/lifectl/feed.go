package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"
)

// feed streams complete lines appended to path into stdout, forever, like
// `tail -f` — except that the line {"type":"eof"} makes it exit (closing the
// reader's stdin). The hub uses it to keep a `claude -p --input-format
// stream-json` process alive between turns and to push the owner's messages into
// a running turn (hub/internal/threads/runner.go).
func feed(path string) {
	const eof = `{"type":"eof"}`
	var off int64
	out := bufio.NewWriter(os.Stdout)
	for {
		f, err := os.Open(path)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		f.Seek(off, io.SeekStart)
		data, _ := io.ReadAll(f)
		f.Close()
		end := strings.LastIndexByte(string(data), '\n')
		if end < 0 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, line := range strings.Split(string(data[:end]), "\n") {
			if strings.TrimSpace(line) == eof {
				out.Flush()
				return
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			out.WriteString(line)
			out.WriteByte('\n')
		}
		out.Flush()
		off += int64(end) + 1
	}
}
