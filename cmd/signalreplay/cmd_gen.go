package signalreplay

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// genFlags holds the flag-bound state shared by "gen" and "run" (run
// generates a config the same way before executing it).
type genFlags struct {
	opts        GenOptions
	exitParams  string
	entryParams string
	out         string
}

func addGenFlags(cmd *cobra.Command, f *genFlags) {
	defaults := DefaultGenOptions()
	*f = genFlags{opts: defaults}

	flags := cmd.Flags()
	flags.StringVar(&f.opts.SignalsPath, "signals", "", "Path to a review sweep CSV (required)")
	flags.StringVar(&f.opts.ExitKind, "exit", "", "Exit strategy kind, e.g. \"chandelier\" (required)")
	flags.StringVar(&f.exitParams, "exit-params", "", "Exit strategy params as key=value[,key=value...]")
	flags.StringVar(&f.opts.Timeframe, "timeframe", defaults.Timeframe, "Candle timeframe for every run")
	flags.StringVar(&f.opts.Source, "source", defaults.Source, "Candle data source")
	flags.Float64Var(&f.opts.RiskPct, "risk-pct", defaults.RiskPct, "Risk percent per trade")
	flags.Float64Var(&f.opts.StartingBalance, "starting-balance", defaults.StartingBalance, "Starting account balance")
	flags.StringVar(&f.opts.AccountCCY, "account-ccy", defaults.AccountCCY, "Account currency")
	flags.Int64Var(&f.opts.Scale, "scale", defaults.Scale, "Price scale (fixed-point denominator)")
	flags.Float64Var(&f.opts.MaxSpreadPips, "max-spread-pips", 0, "Max-spread gate in pips (0 = no gate)")
	flags.IntVar(&f.opts.WarmupDays, "warmup-days", defaults.WarmupDays, "Days of warmup candles before the earliest signal date")
	flags.IntVar(&f.opts.RunoutDays, "runout-days", defaults.RunoutDays, "Days of runout candles after the latest signal date")
	flags.StringVar(&f.opts.Entry, "entry", defaults.Entry, "signalreplay strategy entry mode, e.g. \"next-open\" or \"rejection-candle\"")
	flags.StringVar(&f.entryParams, "entry-params", "", "Entry trigger params as key=value[,key=value...]")
	flags.IntVar(&f.opts.EpisodeGapDays, "episode-gap", defaults.EpisodeGapDays, "signalreplay strategy episode-gap (days)")
	flags.IntVar(&f.opts.MaxHoldDays, "max-hold-days", defaults.MaxHoldDays, "signalreplay strategy max-hold-days (0 = unlimited)")
	flags.BoolVar(&f.opts.CloseOnFlip, "close-on-flip", defaults.CloseOnFlip, "signalreplay strategy close-on-flip")
	flags.BoolVar(&f.opts.OnePerEpisode, "one-per-episode", defaults.OnePerEpisode, "signalreplay strategy one-per-episode")
}

// resolve finalizes exit-params/entry-params parsing once flags are bound;
// must be called after cobra parses flags (RunE, not init time).
func (f *genFlags) resolve() error {
	exitParams, err := ParseExitParams(f.exitParams)
	if err != nil {
		return err
	}
	f.opts.ExitParams = exitParams

	entryParams, err := ParseExitParams(f.entryParams)
	if err != nil {
		return fmt.Errorf("signalreplay gen: --entry-params: %w", err)
	}
	f.opts.EntryParams = entryParams
	return nil
}

func cmdGen() *cobra.Command {
	var f genFlags
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate a backtest YAML config from a review sweep CSV",
		Long: `gen emits a complete, committable, re-runnable backtest YAML: one run per
distinct instrument found in the sweep CSV, with the signalreplay strategy
and the given exit config wired in.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.out == "" {
				return fmt.Errorf("signalreplay gen: --out is required")
			}
			if err := f.resolve(); err != nil {
				return err
			}
			cfg, err := GenerateConfig(f.opts)
			if err != nil {
				return err
			}
			b, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("signalreplay gen: marshal config: %w", err)
			}
			if err := os.WriteFile(f.out, b, 0o644); err != nil {
				return fmt.Errorf("signalreplay gen: write %q: %w", f.out, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%d run%s)\n", f.out, len(cfg.Runs), plural(len(cfg.Runs)))
			return nil
		},
	}
	addGenFlags(cmd, &f)
	cmd.Flags().StringVar(&f.out, "out", "", "Output path for the generated YAML (required)")
	return cmd
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
