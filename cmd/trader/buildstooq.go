package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/service"
)

// errBadRecords fails a Stooq build whose series holds rejected rows, after it has
// published them as bad records: such a series does not load strictly.
var errBadRecords = errors.New("build: the series has bad records and is not complete data")

// runBuildStooq resolves the options of `trader data build --source stooq` and
// hands the request to the application service. Transport only. The data root
// comes from the flag, the environment or the config file; the file's instrument
// list and interval describe the OANDA data and are not used.
func (a *app) runBuildStooq(cmd *cobra.Command, f buildFlags, s stooqFlags) error {
	fl := cmd.Flags()
	if fl.Changed("from") || fl.Changed("to") {
		return errors.New("--from and --to do not apply to --source stooq: the whole history is built")
	}
	if len(f.instruments) != 1 {
		return errors.New("--source stooq needs exactly one --instrument")
	}
	if s.input == "" {
		return errors.New("--source stooq needs --input")
	}
	interval := strings.ToLower(strings.TrimSpace(f.interval))
	if !fl.Changed("interval") {
		interval = "d1"
	}
	cfg, err := loadConfig(f.config, fl.Changed("config"))
	if err != nil {
		return err
	}
	if err := useRoot(resolveRoot(f.dataRoot, a.getenv(envDataRoot), cfg.Marketdata.Root)); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	out := cmd.OutOrStdout()
	var writeErr error // the first failure to write the summary
	say := func(format string, args ...any) {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil && writeErr == nil {
			writeErr = fmt.Errorf("write summary: %w", werr)
		}
	}
	counts := map[service.Action]int{}
	var good, bad int
	req := service.StooqBuildRequest{Instrument: strings.ToUpper(strings.TrimSpace(f.instruments[0])), Interval: interval,
		Input: s.input, Member: s.member, SnapshotAsOf: s.asOf, BuiltAt: time.Now().UTC()}
	built, err := service.BuildStooq(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData), req, func(r service.PartitionResult) {
		counts[r.Action]++
		good += r.Good
		bad += r.Bad
		say("%s: %s, good %d, bad %d\n", filepath.Join(store.Root(), r.Path), r.Action, r.Good, r.Bad)
	})
	if built.Fingerprint != "" {
		say("snapshot: %s as of %s, archived at %s\n", built.Fingerprint, built.SnapshotAsOf, filepath.Join(store.Root(), built.SnapshotPath))
		say("total: built %d, unchanged %d, replaced %d; good bars %d, bad records %d\n",
			counts[service.Built], counts[service.Unchanged], counts[service.Replaced], good, bad)
	}
	if err == nil && bad > 0 {
		err = fmt.Errorf("%w: %d", errBadRecords, bad)
	}
	return errors.Join(err, writeErr)
}
