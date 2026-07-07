package datamanager

import (
	"os"
	"testing"
)

const dukascopyTestsEnv = "TRADER_RUN_DUKASCOPY_TESTS"

func requireDukascopyTests(t *testing.T) {
	t.Helper()
	if os.Getenv(dukascopyTestsEnv) == "1" {
		return
	}
	t.Skip("skipping Dukascopy tests; set TRADER_RUN_DUKASCOPY_TESTS=1 to enable")
}
