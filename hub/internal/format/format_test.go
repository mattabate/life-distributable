package format

import (
	"encoding/json"
	"os"
	"testing"
	"unicode/utf8"
)

// The same table ui.test.js and FormatTests.swift read.
func TestSharedCases(t *testing.T) {
	b, err := os.ReadFile("../../../shared/format-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]json.RawMessage
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	fns := map[string]func(float64) string{
		"usd": USD, "usdWhole": USDWhole, "usdSigned": USDSigned, "usdWholeSigned": USDWholeSigned, "usdShort": USDShort, "usdPrice": Price,
		"commas": func(v float64) string { return Commas(int64(v)) },
	}
	for name, raw := range cases {
		if name == "_" {
			continue
		}
		fn := fns[name]
		if fn == nil {
			t.Fatalf("no Go formatter for %q", name)
		}
		var rows [][2]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(name, err)
		}
		for _, r := range rows {
			if got := fn(r[0].(float64)); got != r[1].(string) {
				t.Errorf("%s(%v) = %q, want %q", name, r[0], got, r[1])
			}
		}
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly", 7, "exactly"},
		{"abcdef", 3, "abc…"},
		{"a—b—c", 2, "a—…"}, // the byte cut split the em dash here
		{"😀😀😀", 1, "😀…"},
		{"héllo", 5, "héllo"}, // 6 bytes, 5 runes: fits
		{"x", 0, "…"},
	} {
		got := Truncate(c.in, c.n)
		if got != c.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("Truncate(%q,%d) is not valid UTF-8", c.in, c.n)
		}
	}
	if got := Tail("ab—cd", 3); got != "—cd" {
		t.Errorf("Tail = %q", got)
	}
}
