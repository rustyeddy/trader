package view

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTable_WidthsSpanHeaderAndAllRows(t *testing.T) {
	tbl := NewTable("PAIR", "ADX")
	tbl.SetRight(1)
	tbl.AddRow("EURUSD", "100.0")
	tbl.AddRow("GBPUSD", "9.0")

	var buf bytes.Buffer
	require.NoError(t, tbl.RenderASCII(&buf))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 4, "header, rule, two rows")
	// "9.0" must be left-padded with a space so it right-aligns under "100.0".
	assert.Contains(t, lines[3], " 9.0")
}

func TestTable_AddGroupInsertsBlankLineBetweenGroupsASCII(t *testing.T) {
	tbl := NewTable("PAIR", "BUCKET")
	tbl.AddRow("EURUSD", "tradeable")
	tbl.AddRow("NZDUSD", "tradeable")
	tbl.AddGroup()
	tbl.AddRow("USDJPY", "hot")

	var buf bytes.Buffer
	require.NoError(t, tbl.RenderASCII(&buf))

	out := buf.String()
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, "\n\nUSDJPY", "blank line separates the two groups")
}

func TestTable_AddGroupInsertsHlineBetweenGroupsOrg(t *testing.T) {
	tbl := NewTable("PAIR", "BUCKET")
	tbl.AddRow("EURUSD", "tradeable")
	tbl.AddGroup()
	tbl.AddRow("USDJPY", "hot")

	var buf bytes.Buffer
	require.NoError(t, tbl.RenderOrg(&buf))

	out := buf.String()
	assert.Contains(t, out, "| PAIR")
	assert.Contains(t, out, "| EURUSD")
	assert.Contains(t, out, "| USDJPY")
	// One hline after the header, one between the two groups.
	assert.Equal(t, 2, strings.Count(out, "\n|-"))
}

func TestTable_NoGroupsRendersOneBlock(t *testing.T) {
	tbl := NewTable("PAIR")
	tbl.AddRow("EURUSD")
	tbl.AddRow("USDJPY")

	var buf bytes.Buffer
	require.NoError(t, tbl.RenderASCII(&buf))
	assert.NotContains(t, buf.String(), "\n\n")
}

func TestTable_EmptyRows(t *testing.T) {
	tbl := NewTable("PAIR", "BUCKET")

	var ascii bytes.Buffer
	require.NoError(t, tbl.RenderASCII(&ascii))
	assert.Equal(t, "PAIR  BUCKET\n----  ------\n", ascii.String())

	var org bytes.Buffer
	require.NoError(t, tbl.RenderOrg(&org))
	assert.Contains(t, org.String(), "| PAIR")
	assert.Equal(t, 0, len(tbl.Lines()), "no groups when there are no rows")
}

func TestTable_RaggedRowsDoNotPanic(t *testing.T) {
	tbl := NewTable("A", "B", "C")
	tbl.AddRow("only-one")

	var buf bytes.Buffer
	assert.NotPanics(t, func() {
		require.NoError(t, tbl.RenderASCII(&buf))
	})
	assert.Contains(t, buf.String(), "only-one")
}

func TestTable_LinesAndOrgLinesAreEmbeddable(t *testing.T) {
	tbl := NewTable("PAIR", "BUCKET")
	tbl.AddRow("EURUSD", "tradeable")
	tbl.AddRow("NZDUSD", "tradeable")
	tbl.AddGroup()
	tbl.AddRow("USDJPY", "hot")

	groups := tbl.Lines()
	require.Len(t, groups, 2)
	assert.Len(t, groups[0], 2)
	assert.Len(t, groups[1], 1)

	orgGroups := tbl.OrgLines()
	require.Len(t, orgGroups, 2)
	assert.Contains(t, orgGroups[0][0], "EURUSD")
	assert.Contains(t, orgGroups[1][0], "USDJPY")
}

func TestTable_SetRightJustifiesOnlyMarkedColumns(t *testing.T) {
	tbl := NewTable("NAME", "COUNT")
	tbl.SetRight(1)
	tbl.AddRow("a", "1")
	tbl.AddRow("bb", "22")

	header := tbl.Header()
	assert.True(t, strings.HasPrefix(header, "NAME"), "left column stays left-justified")
}
