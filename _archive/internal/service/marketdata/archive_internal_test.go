package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
)

// TestArchiveConversion_SelectsConverterByKind: the archive converter is
// chosen by the registered archive kind, and a kind with no converter
// here fails instead of being routed through the Stooq converter
// (PR #457 review, ADR-071).
func TestArchiveConversion_SelectsConverterByKind(t *testing.T) {
	stooqZIP, err := archiveConversion(marketruntime.ProviderInfo{Name: "oanda", Archive: marketruntime.ArchiveNone})
	require.NoError(t, err)
	assert.False(t, stooqZIP, "no archive: build from raw")

	stooqZIP, err = archiveConversion(marketruntime.ProviderInfo{Name: "stooq", Archive: marketruntime.ArchiveStooqZIP})
	require.NoError(t, err)
	assert.True(t, stooqZIP)

	hypothetical := marketruntime.ProviderInfo{Name: "future", Archive: marketruntime.ArchiveKind(99)}
	stooqZIP, err = archiveConversion(hypothetical)
	require.Error(t, err, "an archive kind with no converter must not reach the Stooq converter")
	assert.False(t, stooqZIP)
	assert.ErrorContains(t, err, `provider "future": no converter for archive kind ArchiveKind(99)`)

	// Every registered provider's archive kind has a converter.
	for _, info := range marketruntime.Providers() {
		_, err := archiveConversion(info)
		assert.NoError(t, err, info.Name)
	}
}
