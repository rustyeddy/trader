package review

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// csvFile writes content to a temp file and returns its path.
func csvFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "review.csv")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// validCSV is a minimal two-data-row review CSV (one tradeable, one no-trade).
const validCSV = `Group,Pair,Structure,Setup Bias,Trend,Volatility,Support zone,Resistance Zone,Status
Majors,EURUSD,Bullish,Long,Uptrend,Low,1.0800-1.0820,1.0900-1.0920,Tradeable watch list
Majors,GBPUSD,Neutral,None,Sideways,Medium,1.2500-1.2520,1.2600-1.2620,No Trade
`

// ── New command structure ─────────────────────────────────────────────────────

func TestNew_UseName(t *testing.T) {
	cmd := New(nil)
	assert.Equal(t, "review", cmd.Use)
}

func TestNew_HasFileFlag(t *testing.T) {
	cmd := New(nil)
	assert.NotNil(t, cmd.Flags().Lookup("file"))
}

func TestNew_HasAllFlag(t *testing.T) {
	cmd := New(nil)
	f := cmd.Flags().Lookup("all")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

func TestNew_FileFlagIsRequired(t *testing.T) {
	cmd := New(nil)
	f := cmd.Flags().Lookup("file")
	require.NotNil(t, f)
	// cobra stores required-flag annotations in the command.
	annotations := f.Annotations
	_, required := annotations["cobra_annotation_bash_completion_one_required_flag"]
	assert.True(t, required, "flag --file should be marked required")
}

// ── RunE error paths ──────────────────────────────────────────────────────────

func TestRunE_MissingFileReturnsError(t *testing.T) {
	cmd := New(nil)
	_ = cmd.Flags().Set("file", "/nonexistent/review.csv")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review.csv")
}

func TestRunE_MalformedZoneReturnsError(t *testing.T) {
	bad := `Group,Pair,Structure,Setup Bias,Trend,Volatility,Support zone,Resistance Zone,Status
Majors,EURUSD,Bullish,Long,Uptrend,Low,NOTAZONE,1.0900-1.0920,Tradeable watch list
`
	cmd := New(nil)
	_ = cmd.Flags().Set("file", csvFile(t, bad))
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
}

func TestRunE_ValidCSV_ReturnsNil(t *testing.T) {
	cmd := New(nil)
	_ = cmd.Flags().Set("file", csvFile(t, validCSV))
	require.NoError(t, cmd.RunE(cmd, nil))
}

func TestRunE_EmptyCSV_OnlyHeader_ReturnsNil(t *testing.T) {
	headerOnly := "Group,Pair,Structure,Setup Bias,Trend,Volatility,Support zone,Resistance Zone,Status\n"
	cmd := New(nil)
	_ = cmd.Flags().Set("file", csvFile(t, headerOnly))
	require.NoError(t, cmd.RunE(cmd, nil))
}

// ── --all flag filtering ──────────────────────────────────────────────────────

func TestRunE_AllFlag_DoesNotError(t *testing.T) {
	cmd := New(nil)
	_ = cmd.Flags().Set("file", csvFile(t, validCSV))
	_ = cmd.Flags().Set("all", "true")
	require.NoError(t, cmd.RunE(cmd, nil))
}

func TestRunE_EnDashZone_Parsed(t *testing.T) {
	// Verify the en-dash separator (–, U+2013) that ParseReviewCSV handles.
	enDash := `Group,Pair,Structure,Setup Bias,Trend,Volatility,Support zone,Resistance Zone,Status
Majors,EURUSD,Bullish,Long,Uptrend,Low,1.0800–1.0820,1.0900–1.0920,Tradeable watch list
`
	cmd := New(nil)
	_ = cmd.Flags().Set("file", csvFile(t, enDash))
	require.NoError(t, cmd.RunE(cmd, nil))
}
