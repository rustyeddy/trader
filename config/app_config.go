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

	// ReviewThresholds populated from global config's `review:` section;
	// `trader review`'s own flags override these per-run (see cmd/review).
	ReviewThresholds review.Thresholds

	// OANDA holds broker credentials from global config's `oanda:` section.
	// Commands have no root-level CLI flags for these; each command's own
	// --token/--account-id/--env flags take precedence when set.
	OANDA GlobalOANDAConfig
}
