package trader

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWorkStateMarkClearDownload(t *testing.T) {
	t.Parallel()

	ws := NewWorkState()
	k := Key{Instrument: "EURUSD", Kind: KindTick, TF: Ticks, Year: 2026, Month: 3, Day: 1, Hour: 5}

	require.False(t, ws.IsDownloadQueuedOrActive(k))

	ws.MarkDownload(k)
	require.True(t, ws.IsDownloadQueuedOrActive(k))

	ws.ClearDownload(k)
	require.False(t, ws.IsDownloadQueuedOrActive(k))
}

func TestWorkStateMarkClearBuild(t *testing.T) {
	t.Parallel()

	ws := NewWorkState()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}

	require.False(t, ws.IsBuildQueuedOrActive(k))

	ws.MarkBuild(k)
	require.True(t, ws.IsBuildQueuedOrActive(k))

	ws.ClearBuild(k)
	require.False(t, ws.IsBuildQueuedOrActive(k))
}

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
