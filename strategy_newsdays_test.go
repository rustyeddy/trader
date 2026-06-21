package trader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempNewsFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "newsdays*.txt")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestLoadNewsDays_Basic(t *testing.T) {
	path := writeTempNewsFile(t, "2024-01-05\n2024-03-20\n")
	days, err := LoadNewsDays(path)
	require.NoError(t, err)
	assert.Len(t, days, 2)

	// 2024-01-05 = unix day 19727
	assert.True(t, days[19727])
	// 2024-03-20 = unix day 19802
	assert.True(t, days[19802])
}

func TestLoadNewsDays_CommentsAndBlanks(t *testing.T) {
	content := `# NFP dates
2024-02-02
# FOMC
2024-03-20

2024-05-03  # inline comment
`
	path := writeTempNewsFile(t, content)
	days, err := LoadNewsDays(path)
	require.NoError(t, err)
	assert.Len(t, days, 3)
}

func TestLoadNewsDays_Empty(t *testing.T) {
	path := writeTempNewsFile(t, "# just a comment\n\n")
	days, err := LoadNewsDays(path)
	require.NoError(t, err)
	assert.Empty(t, days)
}

func TestLoadNewsDays_InvalidDate(t *testing.T) {
	path := writeTempNewsFile(t, "2024-01-05\nnot-a-date\n")
	_, err := LoadNewsDays(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid date")
}

func TestLoadNewsDays_FileNotFound(t *testing.T) {
	_, err := LoadNewsDays(filepath.Join(t.TempDir(), "nonexistent.txt"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "news_days_file")
}
