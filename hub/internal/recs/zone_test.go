package recs

import (
	"os"
	"testing"

	"life/hub/internal/store"
)

// The fixtures word times in the hub's zone, as main pins it; without the
// pin the suite failed on any Mac outside New York.
func TestMain(m *testing.M) {
	store.UseEastern()
	os.Exit(m.Run())
}
