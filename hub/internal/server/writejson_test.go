package server

import (
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

// A NaN in an answer is a 500 that says so, never an empty 200.
func TestWriteJSONRefusesNaN(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 200, map[string]float64{"x": math.NaN()})
	if w.Code != 500 || !strings.Contains(w.Body.String(), "could not be encoded") {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	writeJSON(w, 201, map[string]int{"x": 1})
	if w.Code != 201 || strings.TrimSpace(w.Body.String()) != `{"x":1}` {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
}
