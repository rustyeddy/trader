package config

import "github.com/rustyeddy/trader/review"

type RootConfig struct {
	ConfigPath string
	GlobalPath string
	DBPath     string
	ReportPath string
	DataDir    string

	LogLevel  string
	LogFile   string
	LogFormat string
	NoColor   bool

	// OANDA credentials populated from global config; individual commands
	// may override via their own --token / --account-id / --env flags.
	OANDAToken     string
	OANDAAccountID string
	OANDAEnv       string

	// ReviewThresholds populated from global config's `review:` section;
	// `trader review`'s own flags override these per-run (see cmd/review).
	ReviewThresholds review.Thresholds
}
