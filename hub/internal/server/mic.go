package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"life/hub/internal/obs"
)

// The console's microphone: a speech log, one row per phrase, that joins
// to the rest of the owner's observations by time.
//
// The switch is the owner's — the console's Mic button or a lone tap of the
// Option key — and it is a POST here. The helper is ops/bin/mic
// (ops/mic.swift, on-device SpeechAnalyzer, beside the hub binary); the hub
// keeps one WARM: spawned ahead of the tap with the speech model loaded and
// the microphone closed, waiting on stdin. On: the hub writes "go" and the
// input opens within a blink; it reads the helper's JSON lines. Volatile text
// is kept in memory only, for the console to draw; every FINAL result becomes
// one `console/speech` observation (payload {text, started, ended,
// words:[[t0_ms,t1_ms,word]…], device, thread_id?, box?}, ts = the segment's
// start, uniq_key speech:<start ms>). Off: stdin closes; the helper hears
// 0.6 s more for the word in flight, finalizes (the tail lands as a last
// final) and exits; the POST answers with the whole take. Then the next warm
// helper is spawned.
//
// Only the owner's POST ever writes "go": the hub never opens the microphone
// on its own, and the warm helper has no input open (no orange dot) until it
// does. Off is the owner's tap — or micIdle without a word: a take that has
// heard nothing, not even a partial, for that long switches itself off, says
// so in `error` (the console toasts it) and in the log.
// Audio never lands anywhere: the helper keeps none and the hub sees text.
// micIdle: a take without a word for this long was forgotten, not dictated.
const micIdle = 5 * time.Minute

// The mic is a SENSOR, not a feature of one message box: it stays on between
// pages until the owner switches it off. Two things follow here. (1) `on:
// true` while already on is not a no-op: it moves the take to the box the
// caret is in now (`thread_id`), so the rows from here on carry that session
// — the console sends it as the caret moves. (2) The switch survives a hub
// restart: `micOnFile` (beside the DB) holds the thread while the mic is on
// and is removed at off; a hub that boots and finds it switches the mic on
// again, the same way it warms the helper. Idle-off still ends a take nobody
// is speaking into, so a restart while the owner is away costs five minutes
// of an open input, never an evening.
const micOnFile = "mic.on"

// The mic OFF is a send when the owner is away from Sessions: the console
// says so on the tap (`send: {page}`); the hub starts ONE new session with
// the words of the take that went into no box — the ones a message would
// never carry — and the page the mic was switched off on. Only that POST
// sends: a hub restart, the idle switch-off, a helper that dies, a reload or
// a page change never do (none of them is the owner's tap), and a take counts
// from the tap ON (`origin`, which the hub remembers across a restart and the
// console sends back), so a restart mid-take neither splits it into two
// sessions nor loses the words before it. Words are sent once: `sentTo` moves
// to the off, so a second POST off (a double tap, a retry) finds nothing new
// and starts nothing.
const micSendSpan = 3 * time.Hour

// micSwitch is what the switch remembers: the box the words go to (thread =
// its session, box = the console's composer key, "" = no box) and the tap on.
type micSwitch struct {
	thread, box string
	origin      time.Time
}

type micState struct {
	mu sync.Mutex
	// bin: the helper's path; tests point it at a script. keepWarm: spawn the
	// next helper ahead of the tap (set when the real binary is there).
	bin      string
	keepWarm bool
	// onFile: where the switch is remembered across a restart ("" = not).
	onFile string
	on     bool
	// since: when the owner switched it on. started: the helper's audio
	// second 0 in wall clock (its `ready` line), which pins every word's time.
	since, started time.Time
	// heard: the last ready/partial/final; idle: how long without one before
	// the take switches itself off (micIdle unless a test shortens it).
	heard  time.Time
	idle   time.Duration
	device string
	thread string
	// box: the console composer the words go to ("" = none); origin: the tap
	// on, kept across restarts; sentTo: the take is sent up to here.
	box            string
	origin, sentTo time.Time
	// sendMu: one send at a time (a double tap is two POSTs at once).
	sendMu sync.Mutex
	// text: the finals of this take, joined; volatile: the tail still changing.
	text, volatile string
	segments       int
	seq            int
	status, err    string
	// The helper process, warm or on. stopping: stdin closed, exit awaited.
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	warm     bool
	stopping bool
}

// micLine is one JSON line from the helper (ops/mic.swift header).
type micLine struct {
	K      string  `json:"k"`
	Text   string  `json:"text"`
	At     int64   `json:"at"`
	Device string  `json:"device"`
	T0     float64 `json:"t0"`
	T1     float64 `json:"t1"`
	Words  [][]any `json:"words"`
}

type micView struct {
	On       bool   `json:"on"`
	Warm     bool   `json:"warm"`
	Since    string `json:"since,omitempty"`
	Device   string `json:"device,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
	Box      string `json:"box,omitempty"`
	// OriginMS: the tap on (unix ms), the same across a hub restart.
	OriginMS int64  `json:"origin_ms,omitempty"`
	Text     string `json:"text"`
	Volatile string `json:"volatile"`
	Segments int    `json:"segments"`
	Seq      int    `json:"seq"`
	Status   string `json:"status,omitempty"`
	// Stopping: off, but the take is still finalizing (a tap right after a
	// phrase — the last final lands within the second). The console keeps
	// following until it is false again, or that final has no box.
	Stopping bool   `json:"stopping,omitempty"`
	Error    string `json:"error,omitempty"`
	// Sent: the session the tap off started (POST only).
	Sent string `json:"sent,omitempty"`
}

func (s *Server) micStore() *micState {
	s.micOnce.Do(func() {
		s.mic = &micState{}
		if s.cfg != nil && s.cfg.DBPath != "" {
			s.mic.onFile = filepath.Join(filepath.Dir(s.cfg.DBPath), micOnFile)
		}
		if exe, err := os.Executable(); err == nil {
			s.mic.bin = filepath.Join(filepath.Dir(exe), "mic")
			if _, err := os.Stat(s.mic.bin); err == nil {
				s.mic.keepWarm = true
				if sw, was := s.micWasOn(s.mic); was {
					go s.micResume(s.mic, sw)
				} else {
					go s.micWarmUp(s.mic)
				}
			}
		}
	})
	return s.mic
}

// micWasOn: the switch was on when the last hub stopped (its file is there).
// The file is the thread, then a line "<origin ms> <box>".
func (s *Server) micWasOn(m *micState) (micSwitch, bool) {
	if m.onFile == "" {
		return micSwitch{}, false
	}
	b, err := os.ReadFile(m.onFile)
	if err != nil {
		return micSwitch{}, false
	}
	lines := strings.SplitN(string(b), "\n", 3)
	sw := micSwitch{thread: strings.TrimSpace(lines[0])}
	if len(lines) > 1 {
		f := strings.Fields(lines[1])
		if len(f) > 0 {
			var ms int64
			if _, err := fmt.Sscan(f[0], &ms); err == nil && ms > 0 {
				sw.origin = time.UnixMilli(ms)
			}
		}
		if len(f) > 1 {
			sw.box = f[1]
		}
	}
	return sw, true
}

// micResume switches the mic on at boot because it was on at the last stop.
func (s *Server) micResume(m *micState, sw micSwitch) {
	if err := s.micOn(m, sw); err != nil {
		log.Printf("mic: on again after restart: %v", err)
		s.micRemember(m, false, "")
		return
	}
	log.Printf("mic: on again after restart (thread %q)", sw.thread)
}

// micRemember writes the switch beside the DB (on: the thread, origin and
// box, from m — thread is kept as an argument for the off call's shape).
func (s *Server) micRemember(m *micState, on bool, thread string) {
	if m.onFile == "" {
		return
	}
	var err error
	if on {
		err = os.WriteFile(m.onFile, []byte(fmt.Sprintf("%s\n%d %s\n", thread, m.origin.UnixMilli(), m.box)), 0o600)
	} else if err = os.Remove(m.onFile); os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		log.Printf("mic: remember: %v", err)
	}
}

func (m *micState) view() micView {
	v := micView{On: m.on, Warm: m.warm && !m.on, Device: m.device, ThreadID: m.thread, Box: m.box, Text: m.text, Volatile: m.volatile,
		Segments: m.segments, Seq: m.seq, Status: m.status, Stopping: m.stopping, Error: m.err}
	if m.on && !m.origin.IsZero() {
		v.OriginMS = m.origin.UnixMilli()
	}
	if !m.since.IsZero() {
		v.Since = m.since.UTC().Format(time.RFC3339)
	}
	return v
}

func (s *Server) getMic(w http.ResponseWriter, r *http.Request) {
	m := s.micStore()
	m.mu.Lock()
	v := m.view()
	m.mu.Unlock()
	writeJSON(w, 200, v)
}

func (s *Server) postMic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On       bool   `json:"on"`
		ThreadID string `json:"thread_id"`
		Box      string `json:"box"`
		OriginMS int64  `json:"origin_ms"`
		// Send: the tap off on a page other than Sessions (the console decides).
		Send *struct {
			Page string `json:"page"`
		} `json:"send"`
	}
	if !decode(w, r, &in, 1<<20) {
		return
	}
	m := s.micStore()
	var err error
	var sent string
	if in.On {
		sw := micSwitch{thread: in.ThreadID, box: in.Box}
		if in.OriginMS > 0 {
			sw.origin = time.UnixMilli(in.OriginMS)
		}
		err = s.micOn(m, sw)
	} else {
		s.micOff(m)
		if in.Send != nil {
			sent, err = s.micSend(m, in.OriginMS, in.Send.Page)
		}
	}
	m.mu.Lock()
	v := m.view()
	m.mu.Unlock()
	v.Sent = sent
	if err != nil {
		v.Error = err.Error()
		writeJSON(w, 503, v)
		return
	}
	writeJSON(w, 200, v)
}

// micSpawn starts a helper (mutex held). It is warm once it says so.
func (s *Server) micSpawn(m *micState) error {
	if m.bin == "" {
		return fmt.Errorf("no mic helper beside the hub")
	}
	cmd := exec.Command(m.bin)
	// The input choice rides as LIFE_MIC (ops/hub.json mic_input; see config).
	input := "default"
	if s.cfg != nil && s.cfg.MicInput != "" {
		input = s.cfg.MicInput
	}
	cmd.Env = append(os.Environ(), "LIFE_MIC="+input)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mic helper: %w", err)
	}
	m.cmd, m.stdin, m.done, m.warm, m.stopping = cmd, stdin, make(chan struct{}), false, false
	// err is left as it is: the warm-up after an idle switch-off must not
	// erase the reason before the console has polled it (micOn clears it).
	m.status = "loading the speech model"
	log.Printf("mic: helper started (%s)", m.bin)
	go s.micRead(m, bufio.NewScanner(out), cmd, stdin)
	return nil
}

// micWarmUp spawns the helper ahead of the tap, if none is running.
func (s *Server) micWarmUp(m *micState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil || !m.keepWarm {
		return
	}
	if err := s.micSpawn(m); err != nil {
		log.Printf("mic: warm helper: %v", err)
		m.status = ""
	}
}

// micOn opens the input: "go" to the warm helper, or a cold one first.
// Already on = the same take goes on, in the box named now: the rows from
// here carry `thread` (the console sends it as the caret moves).
func (s *Server) micOn(m *micState, sw micSwitch) error {
	thread := sw.thread
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.on {
		if thread != m.thread || sw.box != m.box {
			m.thread, m.box = thread, sw.box
			m.seq++
			s.micRemember(m, true, thread)
			log.Printf("mic: words now for thread %q (box %q)", thread, sw.box)
		}
		return nil
	}
	// The last take may still be finalizing: let it finish rather than lose it.
	for m.stopping {
		done := m.done
		m.mu.Unlock()
		<-done
		m.mu.Lock()
	}
	if m.cmd == nil {
		if err := s.micSpawn(m); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(m.stdin, "go\n"); err != nil {
		return fmt.Errorf("mic helper: %w", err)
	}
	// Field by field: the mutex is held and must stay the same mutex.
	m.on, m.since, m.started, m.device, m.thread, m.box = true, time.Now(), time.Time{}, "", thread, sw.box
	m.text, m.volatile, m.segments, m.err = "", "", 0, ""
	// The take counts from the tap on: a restart's resume or the console's
	// switch-on-again carries it; one older than micSendSpan (or none) is now.
	m.origin = m.since
	if !sw.origin.IsZero() && sw.origin.Before(m.since) && m.since.Sub(sw.origin) < micSendSpan {
		m.origin = sw.origin
	}
	m.heard = m.since
	if m.warm {
		m.status = "starting"
	}
	m.seq++
	s.micRemember(m, true, thread)
	log.Printf("mic: on (thread %q, warm %v)", thread, m.warm)
	go s.micWatch(m, m.since)
	return nil
}

// micWatch ends the take that began at `since` once it has gone idle for
// m.idle (micIdle by default). The owner's tap ending it first ends the watch.
func (s *Server) micWatch(m *micState, since time.Time) {
	m.mu.Lock()
	idle := m.idle
	m.mu.Unlock()
	if idle <= 0 {
		idle = micIdle
	}
	tick := idle / 10
	if tick > 5*time.Second {
		tick = 5 * time.Second
	}
	for {
		time.Sleep(tick)
		m.mu.Lock()
		if !m.on || !m.since.Equal(since) {
			m.mu.Unlock()
			return
		}
		quiet := time.Since(m.heard)
		if quiet < idle {
			m.mu.Unlock()
			continue
		}
		m.err = fmt.Sprintf("mic off by itself: nothing heard for %s", idle.Round(time.Second))
		m.mu.Unlock()
		log.Printf("mic: idle %s, switching off", quiet.Round(time.Second))
		s.micOff(m)
		return
	}
}

// micOff closes the helper's stdin — its signal to hear the tail, finalize
// and exit — and waits for it; a helper that will not stop is killed. Then
// the next helper warms up.
func (s *Server) micOff(m *micState) {
	m.mu.Lock()
	if !m.on {
		m.mu.Unlock()
		return
	}
	m.on = false
	m.stopping = true
	m.status = "stopping"
	stdin, done, cmd := m.stdin, m.done, m.cmd
	s.micRemember(m, false, "")
	m.mu.Unlock()
	if stdin != nil {
		stdin.Close()
	}
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done
	}
	s.micWarmUp(m)
}

// micShutdown ends whatever helper is running (hub drain): a warm one exits
// at EOF, an open one finalizes first.
func (s *Server) micShutdown() {
	if s.mic == nil {
		return
	}
	m := s.mic
	m.mu.Lock()
	m.keepWarm = false
	stdin := m.stdin
	m.mu.Unlock()
	if stdin != nil {
		stdin.Close()
	}
}

// micRead turns the helper's lines into state and rows until it exits.
func (s *Server) micRead(m *micState, sc *bufio.Scanner, cmd *exec.Cmd, stdin io.Closer) {
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var ev micLine
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		m.mu.Lock()
		switch ev.K {
		case "status":
			m.status = ev.Text
			m.seq++
		case "log":
			// Devices and rates: the log's business, never the button's.
			log.Printf("mic: %s", ev.Text)
		case "warm":
			m.warm = true
			if !m.on {
				m.status = ""
			}
			log.Printf("mic: warm (on %v)", m.on)
		case "ready":
			m.started = time.UnixMilli(ev.At)
			m.device = ev.Device
			m.status = "listening"
			m.heard = time.Now()
			m.seq++
		case "partial":
			m.volatile = ev.Text
			m.heard = time.Now()
			m.seq++
		case "final":
			text := strings.TrimSpace(ev.Text)
			m.volatile = ""
			m.heard = time.Now()
			if text != "" {
				if m.text != "" && !strings.HasSuffix(m.text, " ") {
					m.text += " "
				}
				m.text += text
				m.segments++
			}
			m.seq++
			started, thread, box, device := m.started, m.thread, m.box, m.device
			m.mu.Unlock()
			if text != "" {
				s.micRow(started, thread, box, device, text, ev.T0, ev.T1, ev.Words)
			}
			continue
		case "error":
			m.err = ev.Text
			m.status = ""
			log.Printf("mic: helper: %s", ev.Text)
		}
		m.mu.Unlock()
	}
	err := cmd.Wait()
	stdin.Close()
	m.mu.Lock()
	wasOn, wasStopping := m.on, m.stopping
	if err != nil && m.err == "" {
		m.err = "mic helper: " + err.Error()
		log.Printf("mic: %s", m.err)
	}
	// A tail the helper could not finalize is still the owner's words: keep it.
	if tail := strings.TrimSpace(m.volatile); (wasOn || wasStopping) && tail != "" {
		if m.text != "" {
			m.text += " "
		}
		m.text += tail
		m.segments++
		m.volatile = ""
		at := time.Since(m.started).Seconds()
		s.micRow(m.started, m.thread, m.box, m.device, tail, at, at, nil)
	}
	m.on, m.status, m.warm, m.stopping = false, "", false, false
	m.cmd, m.stdin = nil, nil
	m.seq++
	took := time.Since(m.since).Round(time.Second)
	segs := m.segments
	close(m.done)
	// A helper that died on its own ends the take for good; one the hub took
	// down while draining leaves the switch remembered for the next boot.
	if wasOn && !wasStopping {
		select {
		case <-s.stopping:
		default:
			s.micRemember(m, false, "")
		}
	}
	m.mu.Unlock()
	if wasOn || wasStopping {
		log.Printf("mic: off after %s, %d segments", took, segs)
	} else {
		log.Printf("mic: helper exited before any take (%v)", err)
	}
}

// micRow files one final segment as a console/speech observation.
func (s *Server) micRow(started time.Time, thread, box, device, text string, t0, t1 float64, words [][]any) {
	if started.IsZero() {
		started = time.Now().Add(-time.Duration(t1 * float64(time.Second)))
	}
	at := started.Add(time.Duration(t0 * float64(time.Second)))
	end := started.Add(time.Duration(t1 * float64(time.Second)))
	// Word times are relative to the row's own `started`, so a row stands alone.
	ws := make([][]any, 0, len(words))
	for _, w := range words {
		if len(w) != 3 {
			continue
		}
		a, _ := w[0].(float64)
		b, _ := w[1].(float64)
		ws = append(ws, []any{int64(math.Round((a - t0) * 1000)), int64(math.Round((b - t0) * 1000)), w[2]})
	}
	payload := map[string]any{
		"text": text, "started": at.UTC().Format(time.RFC3339Nano), "ended": end.UTC().Format(time.RFC3339Nano),
		"words": ws, "device": device,
	}
	if thread != "" {
		payload["thread_id"] = thread
	}
	// box: the phrase went into a message box (the owner sends that), so a
	// mic-off send leaves it out.
	if box != "" {
		payload["box"] = box
	}
	raw, _ := json.Marshal(payload)
	o := obs.Observation{Source: "console", Kind: "speech", TS: at,
		Payload: raw, UniqKey: fmt.Sprintf("speech:%d", at.UnixMilli())}
	if _, err := s.obs.Insert(o); err != nil && err != obs.ErrDuplicate {
		log.Printf("mic: row: %v", err)
	}
}

// micSend starts one session with the words of the take that went into no box
// (the tap off away from Sessions; the rule is at micSendSpan). The take runs
// from its origin — the hub's, or the console's if earlier (a restart the file
// did not survive) — or from the last send, whichever is later. No such words:
// nothing starts ("" and no error).
func (s *Server) micSend(m *micState, originMS int64, page string) (string, error) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	now := time.Now()
	m.mu.Lock()
	from := m.origin
	if originMS > 0 {
		if o := time.UnixMilli(originMS); (from.IsZero() || o.Before(from)) && now.Sub(o) < micSendSpan {
			from = o
		}
	}
	// A second of slack before the tap (a row's time is its segment's start,
	// pinned by the helper's clock); after a send, only rows past its last.
	if !from.IsZero() {
		from = from.Add(-time.Second)
	}
	if m.sentTo.After(from) {
		from = m.sentTo
	}
	m.mu.Unlock()
	if from.IsZero() || s.thr == nil {
		return "", nil
	}
	rows, err := s.obs.List(obs.Query{Source: "console", Kind: "speech", Since: from, Until: now.Add(time.Second), Limit: 1000})
	if err != nil {
		return "", err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TS.Before(rows[j].TS) })
	var said []string
	var first, last time.Time
	for _, o := range rows {
		var p struct {
			Text   string `json:"text"`
			Thread string `json:"thread_id"`
			Box    string `json:"box"`
		}
		if json.Unmarshal(o.Payload, &p) != nil || p.Thread != "" || p.Box != "" || strings.TrimSpace(p.Text) == "" {
			continue
		}
		if first.IsZero() {
			first = o.TS
		}
		last = o.TS
		said = append(said, strings.TrimSpace(p.Text))
	}
	m.mu.Lock()
	if last.After(m.sentTo) {
		m.sentTo = last.Add(time.Millisecond)
	}
	m.mu.Unlock()
	if len(said) == 0 {
		log.Printf("mic: off on %s, no words outside a box to send", page)
		return "", nil
	}
	name := strings.SplitN(strings.TrimLeft(page, "#/"), "/", 2)[0]
	if name == "" {
		name = "sessions"
	}
	prompt := strings.Join(said, " ") + "\n\n" + fmt.Sprintf("[Spoken, not typed: the owner said this with the console mic on and no message box on their screen, "+
		"%s to %s, then switched the mic off on the %s page (`%s`) — that tap sent it here. "+
		"`lifectl obs speech --source console --since %s` has the phrases with their times. "+
		"Work out what they wanted done on that page, and do it. Thinking aloud with nothing to act on is a quiet `[end]`.]",
		first.In(time.Local).Format("15:04"), last.In(time.Local).Format("15:04"), strings.ToUpper(name[:1])+name[1:], page,
		first.Add(-time.Second).UTC().Format(time.RFC3339))
	t, _, err := s.thr.CreateRouted(prompt, nil, "console")
	if err != nil {
		return "", err
	}
	log.Printf("mic: off on %s sent %d phrases to %s", page, len(said), t.ID)
	return t.ID, nil
}
