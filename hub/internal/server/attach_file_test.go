package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"life/hub/internal/obs"
)

// A file attached to a REPLY on a card arrives as the file (a bank statement
// answered onto an ask from the desktop). The hub's half of that: the upload
// keeps its own extension as an app/upload observation, and the prompt that
// closes the card carries the ref into the session's message, so the runner
// hands the session a .csv to Read.
func TestFileRidesAReplyToACard(t *testing.T) {
	s := newTest(t)
	s.thr.BlobPath = s.obs.BlobPath
	s.obs.IntakeDir = filepath.Join(t.TempDir(), "intake")
	csv := "Account Statement ,,,\nID,Datetime,Amount\n1,2026-09-01,-5.00\n"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("source", "app")
	mw.WriteField("kind", "upload")
	mw.WriteField("payload", `{"via":"session","filename":"Statement_September_2026.csv"}`)
	fw, _ := mw.CreateFormFile("file", "Statement_September_2026.csv")
	fw.Write([]byte(csv))
	mw.Close()
	r := httptest.NewRequest("POST", "/api/v1/observations", &buf)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var o obs.Observation
	json.Unmarshal(w.Body.Bytes(), &o)
	if w.Code != 201 || o.Kind != "upload" || !strings.HasSuffix(o.BlobRef, ".csv") {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	// And the file is in the intake's in-box under its own name, the path on
	// the observation; the same bytes again are the same file, a different
	// file under that name is name-2.
	want := filepath.Join(s.obs.IntakeDir, "Statement_September_2026.csv")
	if got, _ := os.ReadFile(want); string(got) != csv || !strings.Contains(string(o.Payload), `"intake_path":"`+want+`"`) {
		t.Fatalf("no intake copy at %s: %q / %s", want, got, o.Payload)
	}
	if p := s.obs.IntakePath(o.BlobRef); p != want {
		t.Fatalf("IntakePath %q, want %q", p, want)
	}
	if again, _ := s.obs.PutIntake(o.BlobRef, "Statement_September_2026.csv"); again != want {
		t.Fatalf("same bytes again: %q", again)
	}
	ref2, _, _ := s.obs.PutBlob(strings.NewReader(csv+"2,2026-09-02,-6.00\n"), "csv")
	if p, _ := s.obs.PutIntake(ref2, "Statement_September_2026.csv"); p != filepath.Join(s.obs.IntakeDir, "Statement_September_2026-2.csv") {
		t.Fatalf("a different file under a taken name: %q", p)
	}
	if p, _ := s.obs.PutIntake(ref2, "../../etc/passwd"); p != filepath.Join(s.obs.IntakeDir, "passwd.csv") {
		t.Fatalf("a path for a name: %q", p)
	}

	var th struct{ ID string }
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]any{"prompt": "Import September."}).Body.Bytes(), &th)
	var a struct{ ID string }
	json.Unmarshal(s.do(t, "POST", "/api/v1/asks", map[string]any{"thread_id": th.ID, "title": "Download the bank statement", "kind": "physical"}).Body.Bytes(), &a)
	if th.ID == "" || a.ID == "" {
		t.Fatal("no thread or ask")
	}

	// Done, with the file and a line: one message, closing the card.
	if w := s.do(t, "POST", "/api/v1/prompts", map[string]any{"target": "new-or:" + th.ID, "text": "is this it??",
		"in_reply_to": "ask:" + a.ID, "outcome": "done", "attachments": []string{o.BlobRef}}); w.Code != 201 {
		t.Fatalf("prompt: %d %s", w.Code, w.Body)
	}
	if got := s.do(t, "GET", "/api/v1/asks/"+a.ID, nil).Body.String(); !strings.Contains(got, `"state":"done"`) {
		t.Fatal("the card stayed open:", got)
	}
	ms, err := s.thr.Messages(th.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range ms {
		if m.InReplyTo == "ask:"+a.ID {
			found = len(m.Attachments) == 1 && m.Attachments[0] == o.BlobRef && m.Text == "is this it??"
		}
	}
	if !found {
		b, _ := json.Marshal(ms)
		t.Fatalf("the reply carries no .csv: %s", b)
	}

	// A file alone is an answer too: nothing typed, the card still closes
	// and the file still goes.
	json.Unmarshal(s.do(t, "POST", "/api/v1/asks", map[string]any{"thread_id": th.ID, "title": "And October's", "kind": "physical"}).Body.Bytes(), &a)
	if w := s.do(t, "POST", "/api/v1/prompts", map[string]any{"target": "new-or:" + th.ID,
		"in_reply_to": "ask:" + a.ID, "outcome": "done", "attachments": []string{o.BlobRef}}); w.Code != 201 {
		t.Fatalf("wordless prompt: %d %s", w.Code, w.Body)
	}
	ms, _ = s.thr.Messages(th.ID, 10)
	last := ms[len(ms)-1]
	if last.InReplyTo != "ask:"+a.ID || len(last.Attachments) != 1 {
		b, _ := json.Marshal(ms)
		t.Fatalf("a wordless reply dropped its file: %s", b)
	}
}
