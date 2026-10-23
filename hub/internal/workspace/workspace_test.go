package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func store(t *testing.T) *Store {
	roles := map[string]string{"calendar": "assistant", "other-session": "other"}
	return &Store{Dir: filepath.Join(t.TempDir(), "people"),
		Role:  func(id string) string { return roles[id] },
		Known: func(k string) bool { return k == "assistant" || k == "other" || k == "empty" }}
}

// A role's folder is its own: only that session (or the owner) writes, every
// session reads, and an overwrite keeps the old version.
func TestOnlyTheRoleWritesAndNothingIsLost(t *testing.T) {
	s := store(t)
	body := []byte("# Appointments\n\nDentist is Tuesday.")
	if _, err := s.Write("assistant", "appointments.md", body, "claude:thread:other-session"); err != ErrForbidden {
		t.Fatalf("another role wrote the assistant's file: %v", err)
	}
	if _, err := s.Write("assistant", "appointments.md", body, "claude:thread:nobody"); err != ErrForbidden {
		t.Fatalf("a session with no role wrote: %v", err)
	}
	f, err := s.Write("assistant", "appointments.md", body, "claude:thread:calendar")
	if err != nil {
		t.Fatal(err)
	}
	if f.Persona != "assistant" || f.Name != "appointments.md" || f.Head != "Appointments" || f.Bytes == 0 {
		t.Fatalf("%+v", f)
	}
	if _, err := s.Write("assistant", "notes/2026-10.md", []byte("October"), "owner"); err != nil {
		t.Fatal(err)
	}
	if b, err := s.Read("assistant", "appointments.md"); err != nil || !strings.Contains(string(b), "Dentist") {
		t.Fatalf("another session cannot read the file: %v %q", err, b)
	}
	if _, err := s.Write("assistant", "appointments.md", []byte("# Appointments\n\nDentist is Tuesday; haircut Friday."), "claude:thread:calendar"); err != nil {
		t.Fatal(err)
	}
	vs, _ := os.ReadDir(filepath.Join(s.Dir, "assistant", ".versions"))
	if len(vs) != 1 || !strings.HasPrefix(vs[0].Name(), "appointments.md.") {
		t.Fatalf("the old version is not kept: %v", vs)
	}
	all, err := s.List("")
	if err != nil || len(all) != 2 || all[0].Name != "appointments.md" || all[1].Name != "notes/2026-10.md" {
		t.Fatalf("%v %+v", err, all)
	}
	if n := s.Count("assistant"); n != 2 {
		t.Fatal(n)
	}
	if n := s.Count("other"); n != 0 {
		t.Fatal(n)
	}
	if fs, _ := s.List("empty"); len(fs) != 0 {
		t.Fatal("a role with no folder lists nothing")
	}
	if s.Resolve("calendar") != "assistant" || s.Resolve("other") != "other" || s.Resolve("x") != "" {
		t.Fatal("resolve")
	}
}

func TestNamesStayInsideTheFolder(t *testing.T) {
	s := store(t)
	for _, bad := range []string{"", "../x", ".versions/x", "a/.hidden", "a b", "a/b/c/d/e", strings.Repeat("a", 121)} {
		if _, err := s.Write("assistant", bad, []byte("x"), "owner"); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if _, err := s.Write("nope", "x.md", []byte("x"), "owner"); err == nil {
		t.Fatal("unknown persona accepted")
	}
	if _, err := s.Write("assistant", "big", make([]byte, MaxBytes+1), "owner"); err == nil {
		t.Fatal("oversize accepted")
	}
}
