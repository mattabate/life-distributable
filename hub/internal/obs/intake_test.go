package obs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Two different files sent under one name at the same moment (two
// "Statement.pdf" uploads in flight) each keep their own intake copy: the
// name check and the write are one step, so neither overwrites the other,
// and the same bytes sent twice at once still land as one file.
func TestPutIntakeConcurrentSameName(t *testing.T) {
	for round := 0; round < 20; round++ {
		s, _ := newStore(t)
		s.IntakeDir = filepath.Join(t.TempDir(), "intake")
		const n = 16
		refs := make([]string, n)
		for i := range refs {
			ref, _, err := s.PutBlob(bytes.NewReader(bytes.Repeat([]byte(fmt.Sprintf("statement %02d\n", i)), 4096)), "pdf")
			if err != nil {
				t.Fatal(err)
			}
			refs[i] = ref
		}
		var wg sync.WaitGroup
		paths := make([]string, n)
		start := make(chan struct{})
		for i := range refs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				p, err := s.PutIntake(refs[i], "Statement.pdf")
				if err != nil {
					t.Error(err)
				}
				paths[i] = p
			}(i)
		}
		close(start)
		wg.Wait()
		seen := map[string]int{}
		for i, p := range paths {
			if j, dup := seen[p]; dup {
				t.Fatalf("round %d: uploads %d and %d both got %s", round, j, i, p)
			}
			seen[p] = i
			want, _ := os.ReadFile(filepath.Join(s.BlobDir, filepath.FromSlash(refs[i])))
			if got, _ := os.ReadFile(p); !bytes.Equal(got, want) {
				t.Fatalf("round %d: %s does not hold upload %d's bytes", round, p, i)
			}
		}
		if ents, _ := os.ReadDir(s.IntakeDir); len(ents) != n {
			t.Fatalf("round %d: %d files in the intake, want %d", round, len(ents), n)
		}
		// the same bytes sent twice at once are one file
		var p1, p2 string
		wg.Add(2)
		go func() { defer wg.Done(); p1, _ = s.PutIntake(refs[0], "Again.pdf") }()
		go func() { defer wg.Done(); p2, _ = s.PutIntake(refs[0], "Again.pdf") }()
		wg.Wait()
		if p1 != p2 || filepath.Base(p1) != "Again.pdf" {
			t.Fatalf("round %d: one file sent twice landed as %s and %s", round, p1, p2)
		}
	}
}
