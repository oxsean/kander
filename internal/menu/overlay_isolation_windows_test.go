package menu

import (
	"os"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// TestMain mirrors the POSIX harness on Windows, where that harness is excluded:
// fixtures keep their own scope configuration instead of merging the project
// overlay of the checkout the tests run in.
func TestMain(m *testing.M) {
	if err := os.Setenv(config.EnvNoOverlay, "1"); err != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}
