// Package brand is the small coloured tile a data source wears on the
// Configuration page, so a card is told apart before a word is read.
//
// Decided HERE once and drawn as served by both surfaces (ui.js brandMark,
// ConfigView.swift BrandTile). A mark is letters on the provider's own
// colour, or an embedded glyph: never a fetched logo, so drawing a card tells
// nobody anything.
package brand

import (
	"strings"
	"unicode"
)

// Mark is one provider's tile: 1–4 characters, the tile colour, the ink on it.
type Mark struct {
	Mark  string `json:"mark"`
	Color string `json:"color"`
	Ink   string `json:"ink"`
	// Logo, when there is one: an SVG path in a 24×24 box, drawn in Ink in
	// place of the letters (logos.go). Mark stays as the fallback text.
	Logo string `json:"logo,omitempty"`
}

// tile is a Mark without its logo, so the tables below stay one short line
// a row.
type tile struct{ Mark, Color, Ink string }

func (t tile) mark() Mark { return Mark{Mark: t.Mark, Color: t.Color, Ink: t.Ink} }

// Keyed by the name squeezed to lowercase letters and digits, so "Apple
// Health", "apple_health" and "APPLE HEALTH" are one row.
func squeeze(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// For returns a tile for any name: its initials on slate. A source the hub
// has no tile for is still never a card without one.
func For(name string) Mark {
	initials := ""
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(initials)) < 2 {
			initials += strings.ToUpper(string([]rune(w)[:1]))
		}
	}
	if initials == "" {
		initials = "?"
	}
	return tile{initials, "#64748B", "#FFFFFF"}.mark()
}

// The data sources' tiles, keyed by the hub's source id. A source an owner's
// agent adds (a bank feed, a mail box, a calendar) gets its row here and, if
// a free logo exists, one in logos.go.
var sourceMarks = map[string]tile{
	"anthropic":  {"A", "#D97757", "#FFFFFF"},
	"github":     {"GH", "#181717", "#FFFFFF"},
	"mail":       {"M", "#EA4335", "#FFFFFF"},
	"gcal":       {"31", "#1A73E8", "#FFFFFF"},
	"hub_tables": {"HT", "#0F766E", "#FFFFFF"},
	"health":     {"♥", "#FF2D55", "#FFFFFF"},
	"app":        {"L", "#16A34A", "#FFFFFF"},
}

// ForSource is a data source's tile: its own when it has one, else initials
// on slate. The logo, when logos.go has one, rides along; the letters stay
// as its fallback.
func ForSource(id, title string) Mark {
	t, ok := sourceMarks[squeeze(id)]
	if !ok {
		return For(title)
	}
	m := t.mark()
	m.Logo = sourceLogos[squeeze(id)]
	return m
}
