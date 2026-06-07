package trader

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPlanLog(t *testing.T) {
	t.Parallel()

	// Just verify Log doesn't panic with various states
	p := Plan{}
	require.NotPanics(t, p.Log)

	p2 := Plan{
		Download: []Key{{Instrument: "EURUSD"}},
		BuildM1:  []BuildTask{{Kind: BuildM1}},
		BuildH1:  []BuildTask{{Kind: BuildH1}},
		BuildD1:  []BuildTask{{Kind: BuildD1}},
	}
	require.NotPanics(t, p2.Log)
}
