package data_test

import (
	"os"
	"testing"

	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
)

func TestMain(m *testing.M) {
	// Hermetic: never read a developer's or a server's real config file.
	marketdatacfg.DefaultConfigPath = ""
	os.Exit(m.Run())
}
