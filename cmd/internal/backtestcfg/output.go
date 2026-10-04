package backtestcfg

import (
	"github.com/rustyeddy/trader/internal/config"
)

// DefaultOutputDir is where run snapshots go when nothing configures
// otherwise.
const DefaultOutputDir = "./backtest-runs"

// outputConfig is the run-snapshot directory setting, resolved the same
// way by every binary that reads or writes run snapshots (`trader
// backtest run`/`show` and trader-mcp): backtest.output_dir in a config
// file, TRADER_BACKTEST_OUTPUT_DIR, or the output-dir override.
type outputConfig struct {
	Backtest struct {
		OutputDir string `config:"output_dir" flag:"output-dir" default:"./backtest-runs"`
	}
}

// LoadOutputDir resolves the run-snapshot directory: override (a flag
// value; empty means not given) wins, then TRADER_BACKTEST_OUTPUT_DIR in
// environ, then backtest.output_dir in the config file at filePath (if
// any), then DefaultOutputDir.
func LoadOutputDir(environ []string, filePath, override string) (string, error) {
	overrides := map[string]string{}
	if override != "" {
		overrides["output-dir"] = override
	}
	cfg, err := config.Load[outputConfig](config.Options{
		EnvPrefix: "TRADER",
		Environ:   environ,
		FilePath:  filePath,
		Overrides: overrides,
	})
	if err != nil {
		return "", err
	}
	return cfg.Backtest.OutputDir, nil
}
