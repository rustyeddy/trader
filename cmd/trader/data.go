package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/marketdata/oanda"
)

const (
	// envOANDAToken is the only place the OANDA credential comes from
	// (ADR-078 S-1): there is deliberately no flag and no config key for it.
	envOANDAToken = "TRADER_OANDA_TOKEN"
	envDataRoot   = "TRADER_DATA_ROOT"

	// defaultBaseURL is OANDA's practice host, so a run without a base URL can
	// only read demo data; the live host is chosen explicitly.
	defaultBaseURL = "https://api-fxpractice.oanda.com"

	// defaultRequestGap spaces provider requests so a long download stays well
	// inside OANDA's limits. A 401 is still never retried: it is a permanent
	// error (the credential), not something pacing or retrying can fix.
	defaultRequestGap = 500 * time.Millisecond
)

// newDataCmd is the "data" command group. cmd/trader is the composition root
// (ADR-089): the one place that reads the environment and the config file and
// builds the logger and the credential the packages are given.
func newDataCmd(a *app) *cobra.Command {
	data := &cobra.Command{
		Use:   "data",
		Short: "Market data commands",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	data.AddCommand(newDownloadCmd(a))
	return data
}

type downloadFlags struct {
	config      string
	dataRoot    string
	instruments []string
	interval    string
	from, to    string
	baseURL     string
}

func newDownloadCmd(a *app) *cobra.Command {
	var f downloadFlags
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download OANDA candles into the raw store",
		Long: `Download OANDA bid/ask candles for a UTC range into the raw store, one month
partition file at a time:

  <data root>/oanda/raw/<instrument>/<YYYY>/<MM>/<instrument>-<YYYY>-<MM>-<interval>.csv

Rows already on file are never changed; only missing rows are added, and a
candle the provider still reports as forming is never stored (a price that is
still moving belongs to a tick feed, not to the immutable candle archive). The token is read
only from ` + envOANDAToken + `. The data root is --data-root, then ` + envDataRoot + `, then
marketdata.root in ` + defaultConfigPath + `; --instrument, --interval and --base-url also
fall back to that file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runDownload(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file)")
	fl.StringSliceVar(&f.instruments, "instrument", nil, "instrument, such as EURUSD; repeat or separate with commas (default: marketdata.instruments)")
	fl.StringVar(&f.interval, "interval", "", "m1, h1, h4 or d1 (default: marketdata.interval)")
	fl.StringVar(&f.from, "from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.to, "to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.baseURL, "base-url", "", "OANDA REST host (default: marketdata.base_url, then the practice host)")
	return cmd
}

// runDownload resolves every option (flag, environment, file), builds the
// client with the credential injected, and downloads.
func (a *app) runDownload(cmd *cobra.Command, f downloadFlags) error {
	fl := cmd.Flags()
	cfg, err := loadConfig(f.config, fl.Changed("config"))
	if err != nil {
		return err
	}
	md := cfg.Marketdata
	if md.Provider != "" && md.Provider != "oanda" {
		return fmt.Errorf("config: marketdata.provider %q is not supported (only oanda)", md.Provider)
	}

	root, err := resolveRoot(f.dataRoot, a.getenv(envDataRoot), md.Root)
	if err != nil {
		return err
	}
	symbols := pick(fl.Changed("instrument"), f.instruments, md.Instruments)
	if len(symbols) == 0 {
		return errors.New("no instrument: set --instrument or marketdata.instruments in " + defaultConfigPath)
	}
	token := pickString(fl.Changed("interval"), f.interval, md.Interval)
	if token == "" {
		return errors.New("no interval: set --interval or marketdata.interval in " + defaultConfigPath)
	}
	from, to, err := parseRange(f.from, f.to)
	if err != nil {
		return err
	}
	baseURL := pickString(fl.Changed("base-url"), f.baseURL, md.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if a.getenv(envOANDAToken) == "" {
		return errors.New(envOANDAToken + " is not set (the token is read only from the environment)")
	}

	req := downloadRequest{Symbols: symbols, Interval: oanda.RawInterval(strings.ToLower(token)), From: from, To: to}
	for _, s := range req.Symbols {
		if err := (oanda.PartitionKey{Symbol: s, Interval: req.Interval, Year: from.Year(), Month: from.Month()}).Validate(); err != nil {
			return fmt.Errorf("--instrument %q / --interval %q: %w", s, token, err)
		}
	}

	log := slog.Default()
	client, err := oanda.NewClient(oanda.ClientConfig{
		BaseURL:    baseURL,
		Credential: tokenFromEnv{getenv: a.getenv},
		Logger:     log,

		MinRequestInterval: a.requestGap,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	out := cmd.OutOrStdout()
	var total partitionResult
	var writeErr error // the first failure to write the summary
	say := func(format string, args ...any) {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil && writeErr == nil {
			writeErr = fmt.Errorf("write summary: %w", werr)
		}
	}
	err = downloadMonths(ctx, logging.WithComponent(log, logging.ComponentMarketData), client, root, req, func(r partitionResult) {
		total.Fetched += r.Fetched
		total.Added += r.Added
		total.Kept += r.Kept
		total.Incomplete += r.Incomplete
		total.Differing += r.Differing
		say("%s: fetched %d, added %d, kept %d, not stored (incomplete) %d\n",
			r.Path, r.Fetched, r.Added, r.Kept, r.Incomplete)
	})
	say("total: fetched %d, added %d, kept %d, not stored (incomplete) %d, stored rows that differ from the provider %d\n",
		total.Fetched, total.Added, total.Kept, total.Incomplete, total.Differing)
	return errors.Join(err, writeErr)
}

// tokenFromEnv asks the environment for the token on every request, so the
// client never holds it.
type tokenFromEnv struct{ getenv func(string) string }

func (t tokenFromEnv) Token(context.Context) (string, error) {
	return t.getenv(envOANDAToken), nil
}

// pick returns the flag's values when the flag was given, else the file's.
func pick(flagSet bool, flag, file []string) []string {
	if flagSet {
		return flag
	}
	return file
}

func pickString(flagSet bool, flag, file string) string {
	if flagSet {
		return strings.TrimSpace(flag)
	}
	return strings.TrimSpace(file)
}

// parseRange reads --from and --to, each a UTC date or an RFC 3339 instant.
func parseRange(from, to string) (time.Time, time.Time, error) {
	if from == "" || to == "" {
		return time.Time{}, time.Time{}, errors.New("--from and --to are required")
	}
	f, err := parseInstant(from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("--from: %w", err)
	}
	t, err := parseInstant(to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("--to: %w", err)
	}
	if !t.After(f) {
		return time.Time{}, time.Time{}, errors.New("--to must be after --from")
	}
	return f, t, nil
}

func parseInstant(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not YYYY-MM-DD or an RFC 3339 instant", s)
	}
	return t.UTC(), nil
}
