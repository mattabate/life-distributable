package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"life/hub/internal/threads"
)

func TestSessionBlobUploadPreservesRawBytes(t *testing.T) {
	for name, body := range map[string]string{
		"object": "{\n  \"by\": \"analyst\", \"value\": 42\n}\n",
		"null":   "null\n",
		"empty":  "",
	} {
		t.Run(name, func(t *testing.T) {
			s := newTest(t)
			s.cfg.SessionTokens = true
			r := httptest.NewRequest("POST", "/api/v1/blobs?ext=json", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+SessionToken(s.token, "th-review"))
			r.Header.Set("Content-Type", "application/octet-stream")
			w := httptest.NewRecorder()
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("raw upload panicked: %v", p)
				}
			}()
			s.ServeHTTP(w, r)
			if body == "" {
				if w.Code != 400 {
					t.Fatalf("empty upload: %d %s", w.Code, w.Body)
				}
				return
			}
			if w.Code != 201 {
				t.Fatalf("upload: %d %s", w.Code, w.Body)
			}
			var result struct {
				Ref   string `json:"ref"`
				Bytes int    `json:"bytes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			path, err := s.obs.BlobPath(result.Ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != body || result.Bytes != len(body) {
				t.Fatalf("stored %q (%d bytes), want %q (%d bytes)", got, result.Bytes, body, len(body))
			}
		})
	}
}

func TestSessionWorkspaceWritePreservesRawBytes(t *testing.T) {
	s := newTest(t)
	th, err := s.thr.Create("Review", "life", "", "Review.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.thr.Update(th.ID, map[string]string{"persona": threads.PersonaAssistant}); err != nil {
		t.Fatal(err)
	}
	s.cfg.SessionTokens = true
	body := "{\n  \"by\": \"analyst\", \"value\": 42\n}\n"
	r := httptest.NewRequest("PUT", "/api/v1/workspace/assistant/notes.md", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+SessionToken(s.token, th.ID))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("write: %d %s", w.Code, w.Body)
	}
	got, err := s.WS.Read(threads.PersonaAssistant, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("stored %q, want %q", got, body)
	}
}

func TestSessionJSONNullReturnsClientError(t *testing.T) {
	s := newTest(t)
	s.cfg.SessionTokens = true
	r := httptest.NewRequest("POST", "/api/v1/goals", strings.NewReader("null"))
	r.Header.Set("Authorization", "Bearer "+SessionToken(s.token, "th-review"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("JSON null panicked instead of returning a client error: %v", p)
		}
	}()
	s.ServeHTTP(w, r)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status %d, want a client error: %s", w.Code, w.Body)
	}
}
