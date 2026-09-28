// steerprobe checks the assumption the thread runner relies on: that a user
// message written to `claude -p --input-format stream-json` stdin while a
// turn is in flight is picked up inside that turn (mid-turn steering). Run
// after upgrading claude:
//
//	go run ./cmd/steerprobe [path-to-claude]
//
// The final assistant text should quote the STEER message; the number of
// `result` lines tells whether a steered message yields its own turn.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	bin := "claude"
	if len(os.Args) > 1 {
		bin = os.Args[1]
	}
	cmd := exec.Command(bin, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--allowedTools", "Bash", "--model", "claude-haiku-4-5-20251001")
	cmd.Dir = os.TempDir()
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Println("start:", err)
		os.Exit(1)
	}
	results := 0
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			if strings.Contains(line, `"type":"result"`) {
				results++
			}
			if len(line) > 300 {
				line = line[:300] + "…"
			}
			fmt.Printf("%s %s\n", time.Now().Format("15:04:05"), line)
		}
		close(done)
	}()
	send := func(text string) {
		fmt.Printf("%s >> %s\n", time.Now().Format("15:04:05"), text)
		io.WriteString(stdin, `{"type":"user","message":{"role":"user","content":`+fmt.Sprintf("%q", text)+"}}\n")
	}
	send("Run exactly this with Bash, once, verbatim: end=$(($(date +%s)+25)); until [ $(date +%s) -ge $end ]; do sleep 1; done; echo waited\nAfter it finishes, tell me whether you received any additional message from me while it was running, and quote it if so. Then stop.")
	time.Sleep(8 * time.Second)
	send("STEER: the secret word is pineapple.")
	time.Sleep(50 * time.Second)
	send("Reply with one word: ok.")
	time.Sleep(15 * time.Second)
	send("Reply with one word: ok.")
	time.Sleep(15 * time.Second)
	stdin.Close()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
	}
	fmt.Println("result lines:", results)
}
