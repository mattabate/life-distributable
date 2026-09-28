package recs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// Recs are pull, never push (docs/design/recommendations.md: no push, no
// card, ever). The rule holds by construction: this package cannot reach the
// push funnel, and never calls anything that raises a card (an ask or a
// step). flow.go does talk to sessions — it hands the owner's decision back
// to the session that filed the rec — but that is the answer travelling away
// from them, never a new thing asking them. Merging the tables
// (step 9) put a rec in the same `items` table as a card; this keeps it from
// ever being one.
func TestRecsCannotPush(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		for name, f := range p.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, imp := range f.Imports {
				if path, _ := strconv.Unquote(imp.Path.Value); path == "life/hub/internal/notify" {
					t.Errorf("%s imports notify: a rec must never push", name)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if m := sel.Sel.Name; strings.HasPrefix(m, "AddAsk") || m == "RaiseStep" || m == "Notify" || m == "Push" {
						t.Errorf("%s calls %s: a rec must never become a card", name, m)
					}
				}
				return true
			})
		}
	}
}
