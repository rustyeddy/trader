package numericstudy

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Defensive branches in the rendering and range helpers.  They are guards
// against nonsense input rather than paths the studies take, but an untested
// guard is just an assumption.

func TestHeadroomNonPositive(t *testing.T) {
	sc := scaleByName(t, "1e8")

	assert.Zero(t, Headroom(0, sc), "zero has no headroom to report")
	assert.Zero(t, Headroom(-1, sc), "a negative price is not a headroom question")
	assert.Zero(t, Headroom(math.MinInt64, sc))

	assert.Positive(t, Headroom(1, sc))
}

func TestMaxPriceCeilNonPositive(t *testing.T) {
	assert.Zero(t, MaxPriceCeil(0))
	assert.Zero(t, MaxPriceCeil(-1))
	assert.Zero(t, MaxPriceCeil(math.MinInt64))

	assert.Equal(t, int64(math.MaxInt64), MaxPriceCeil(1))
}

func TestCommas(t *testing.T) {
	cases := map[int64]string{
		0:             "0",
		1:             "1",
		999:           "999",
		1000:          "1,000",
		1234567:       "1,234,567",
		-1:            "-1",
		-1000:         "-1,000",
		-1234567:      "-1,234,567",
		math.MaxInt64: "9,223,372,036,854,775,807",
		math.MinInt64: "-9,223,372,036,854,775,808",
	}

	for in, want := range cases {
		assert.Equal(t, want, Commas(in), "Commas(%d)", in)
	}
}

func TestValidateTableRejects(t *testing.T) {
	cases := []struct {
		name, body, wantErr string
	}{
		{
			name:    "too few lines",
			body:    "| A |\n|---|\n",
			wantErr: "at least one row",
		},
		{
			name:    "empty",
			body:    "",
			wantErr: "at least one row",
		},
		{
			name:    "line is not a row",
			body:    "| A |\n|---|\nnot a row\n",
			wantErr: "not a table row",
		},
		{
			name:    "column count differs",
			body:    "| A | B |\n|---+---|\n| 1 |\n",
			wantErr: "columns, want",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTable(c.body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantErr)
		})
	}
}

func TestValidateTableAccepts(t *testing.T) {
	assert.NoError(t, ValidateTable("| A | B |\n|---+---|\n| 1 | 2 |\n"))
	assert.NoError(t, ValidateTable("| A | B |\n|---|--:|\n| 1 | 2 |\n"))
}

func TestRenderTableRaggedColumns(t *testing.T) {
	// A column shorter than its neighbours pads with blanks rather than
	// dropping rows, so a data bug shows up as a gap instead of silent loss.
	body := RenderTable(FormatOrg, []Column{
		{Header: "A", Cells: []string{"1", "2", "3"}},
		{Header: "B", Cells: []string{"x"}},
	})

	require.NoError(t, ValidateTable(body))
	assert.Equal(t, 5, len(strings.Split(strings.TrimRight(body, "\n"), "\n")),
		"header, rule, and three rows")
	assert.Contains(t, body, "| 3 |   |")
}

func TestFormatFixedNegativeDecimals(t *testing.T) {
	sc := scaleByName(t, "1e8")
	v := int64(150_000_000) // 1.5

	assert.Equal(t, "1", FormatFixed(v, sc, 0))
	assert.Equal(t, "1", FormatFixed(v, sc, -1), "negative precision clamps to zero")
	assert.Equal(t, "1", FormatFixed(v, sc, math.MinInt32))
}

func TestMustScalePanicsOnUnknown(t *testing.T) {
	assert.PanicsWithValue(t, "numericstudy: unknown scale 1e42", func() {
		mustScale("1e42")
	})

	assert.NotPanics(t, func() { mustScale("1e8") })
}
