package config

import (
	"os"
	"testing"
)

// TestMain keeps each fixture's own scope configuration authoritative: without
// this the lookup would reach the project overlay of the checkout the tests run
// in and merge a developer's settings into every fixture. Tests that exercise
// the lookup itself re-enable it with overlayLookup.
func TestMain(m *testing.M) {
	if err := os.Setenv(EnvNoOverlay, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func overlayLookup(t *testing.T) {
	t.Helper()
	t.Setenv(EnvNoOverlay, "")
}
