package threads

import "testing"

func TestListifyDetail(t *testing.T) {
	in := "One key covers both. 1) console.example.com → sign in. 2) Project picker → New project (v3.1). 3) Enable it. Then reply here."
	want := "One key covers both.\n1. console.example.com → sign in.\n2. Project picker → New project (v3.1).\n3. Enable it. Then reply here."
	if got := ListifyDetail(in); got != want {
		t.Fatalf("got %q", got)
	}
	for _, s := range []string{"plain prose, 2 things in 2024.", "already\n1. a\n2. b", "only 1) one step", "out of order 2) x 1) y"} {
		if got := ListifyDetail(s); got != s {
			t.Fatalf("%q changed to %q", s, got)
		}
	}
}
