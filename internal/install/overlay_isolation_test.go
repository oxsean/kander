package install

import (
	"os"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// TestMain keeps each fixture's own scope configuration authoritative: without
// this the lookup would reach the project overlay of the checkout the tests run
// in and merge a developer's settings into every fixture.
func TestMain(m *testing.M) {
	if err := os.Setenv(config.EnvNoOverlay, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
