package store

import (
	"errors"
	"testing"
)

// A close of the owner's is immutable to sessions: no relabel (done to
// dismissed or back), a reopen only with words; the owner's own surfaces and
// the hub's reconcilers still move it.
func TestGuardOwnerClose(t *testing.T) {
	sess := "claude:thread:t1"
	cases := []struct {
		name                           string
		from, resolvedBy, to, by, note string
		refused                        bool
	}{
		{"session relabels owner done", "done", "app", "dismissed", sess, "moot", true},
		{"session relabels owner dismissed", "dismissed", "web", "done", sess, "did it", true},
		{"session reopens without words", "done", "app", "open", sess, " ", true},
		{"session reopens with the reason", "done", "app", "open", sess, "the copy hung", false},
		{"session rewrites its own close", "done", sess, "dismissed", sess, "moot", false},
		{"hub close of the owner's row", "done", "app", "open", "hub", "", false},
		{"owner reopens", "done", "app", "open", "owner", "", false},
		{"an open row is anyone's", "open", "", "done", sess, "", false},
	}
	for _, c := range cases {
		err := GuardOwnerClose(c.from, c.resolvedBy, c.to, c.by, c.note)
		if c.refused != errors.Is(err, ErrOwnerClose) || (!c.refused && err != nil) {
			t.Errorf("%s: refused=%v want %v (%v)", c.name, err != nil, c.refused, err)
		}
	}
}
