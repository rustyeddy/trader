package backtest

import (
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
)

func TestBacktestRunConfigPathPrecedence(t *testing.T) {
	base := filepath.Join("srv", "trading", "backtests")
	root := &trader.RootConfig{ConfigPath: "root-config.yml"}

	assert.Equal(t, "arg-config.yml",
		backtestRunConfigPath(base, []string{"arg-config.yml"}, "local-config.yml", root))
	assert.Equal(t, "local-config.yml",
		backtestRunConfigPath(base, nil, " local-config.yml ", root))
	assert.Equal(t, "root-config.yml",
		backtestRunConfigPath(base, nil, "", root))
	assert.Equal(t, filepath.Join(base, "configs"),
		backtestRunConfigPath(base, nil, "", nil))
}
