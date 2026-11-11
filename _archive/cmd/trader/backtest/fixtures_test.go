package backtest_test

import "path/filepath"

// rawFixtureRoot is the repository's shared synthetic OANDA raw archive
// (testdata/data/oanda/raw, the provider-first layout of
// docs/arch/market-data-storage.md), relative to this package. It is shared
// by every backtest test and the EMA-cross regression, not owned by one.
const rawFixtureRoot = "../../../testdata/data/oanda/raw"

// rawFixtureAbs is the same directory as an absolute path, for config files
// written to a temporary directory, where a relative data_raw_root would
// resolve against that directory (it resolves against its config file).
var rawFixtureAbs = func() string {
	p, err := filepath.Abs(rawFixtureRoot)
	if err != nil {
		panic(err)
	}
	return p
}()
