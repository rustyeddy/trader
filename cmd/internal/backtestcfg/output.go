package backtestcfg

import (
	"path/filepath"

	"github.com/rustyeddy/trader/internal/config"
)

// DefaultOutputDir is where run snapshots go when nothing configures
// otherwise.
const DefaultOutputDir = "./backtest-runs"

// DefaultOutputFormat is how a run's report is rendered when nothing
// configures otherwise.
const DefaultOutputFormat = "table"

// outputConfig is where a run's result goes and how it is rendered, resolved
// the same way by every binary that reads or writes run snapshots (`trader
// backtest run`/`show` and trader-mcp): backtest.output_dir and
// backtest.output_format in a config file, TRADER_BACKTEST_OUTPUT_DIR and
// TRADER_BACKTEST_OUTPUT_FORMAT, or the output-dir and format overrides.
type outputConfig struct {
	Backtest struct {
		OutputDir    string `config:"output_dir" flag:"output-dir" default:"./backtest-runs"`
		OutputFormat string `config:"output_format" flag:"format" default:"table"`
	}
}

// Output is a run's resolved result location and report format.
type Output struct {
	// Dir is where run snapshots are written and read.
	Dir string
	// Format is how the run's report is rendered to stdout.
	Format string
}

// LoadOutput resolves the output settings: an override (a flag value; empty
// means not given) wins, then the TRADER_BACKTEST_* environment variables in
// environ, then the config file at filePath (if any), then the defaults. A
// relative output_dir written in the config file is relative to that file
// (see LoadRunConfig); one from a flag, the environment or the default is
// relative to the working directory.
func LoadOutput(environ []string, filePath, dirOverride, formatOverride string) (Output, error) {
	overrides := map[string]string{}
	if dirOverride != "" {
		overrides["output-dir"] = dirOverride
	}
	if formatOverride != "" {
		overrides["format"] = formatOverride
	}
	opts := config.Options{
		EnvPrefix: "TRADER",
		Environ:   environ,
		FilePath:  filePath,
		Overrides: overrides,
	}
	cfg, err := config.Load[outputConfig](opts)
	if err != nil {
		return Output{}, err
	}
	dir := fileRelative(opts, readDocument(opts), "backtest", "output_dir", "output-dir", cfg.Backtest.OutputDir)
	return Output{Dir: dir, Format: cfg.Backtest.OutputFormat}, nil
}

// LoadOutputDir resolves only the run-snapshot directory, for callers that
// have no report to render (trader backtest show, trader-mcp).
func LoadOutputDir(environ []string, filePath, override string) (string, error) {
	out, err := LoadOutput(environ, filePath, override, "")
	return out.Dir, err
}

// fileRelative resolves a path-valued key against the directory of the
// config file that wrote it, so a checked-in config names its fixtures and
// outputs relative to itself and runs the same from any working directory.
// Only a value that came from the file is resolved: an absolute path, the
// default, and anything set by a flag or the environment are returned
// unchanged, since those are the caller's to interpret.
func fileRelative(opts config.Options, doc map[string]any, section, key, flag, value string) string {
	if opts.FilePath == "" || value == "" || filepath.IsAbs(value) {
		return value
	}
	if _, ok := opts.Overrides[flag]; ok {
		return value
	}
	if _, ok := environ(opts)[envName(opts.EnvPrefix, section+"."+key)]; ok {
		return value
	}
	sec, _ := doc[section].(map[string]any)
	if raw, ok := sec[key].(string); !ok || raw != value {
		return value
	}
	return filepath.Join(filepath.Dir(opts.FilePath), value)
}
