// Package bot hosts CLI subcommands for managing live strategy bots.
//
// Server mode (default): commands are thin HTTP clients that talk to a running
// trader serve process via the REST API.
//
// Local mode (--local flag on bot start): the bot runs directly as a goroutine
// in the CLI process — no server required. Output goes to stdout and Ctrl-C
// stops the bot cleanly. Use this for ad-hoc or development runs.
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/log"
	accountsvc "github.com/rustyeddy/trader/service/account"
	botsvc "github.com/rustyeddy/trader/service/bots"
	"github.com/rustyeddy/trader/strategy"
)

var serverURL string

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bot",
		Short: "Manage live strategy bots",
	}
	cmd.PersistentFlags().StringVar(&serverURL, "server", defaultServer(), "trader serve base URL (server mode)")
	cmd.AddCommand(
		botListCmd(),
		botGetCmd(),
		botStartCmd(rc),
		botStopCmd(),
		botReportCmd(),
	)
	return cmd
}

func defaultServer() string {
	if u := os.Getenv("TRADER_SERVER"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// ── bot list ──────────────────────────────────────────────────────────────

func botListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all bots (running and stopped) on the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			var statuses []botsvc.BotStatus
			if err := apiGet(serverURL+"/api/v1/bots", &statuses); err != nil {
				return err
			}
			if len(statuses) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No bots found.")
				return nil
			}
			out := cmd.OutOrStdout()
			bar := strings.Repeat("─", 68)
			fmt.Fprintln(out, bar)
			fmt.Fprintf(out, "  %-12s %-12s %-16s %-10s %s\n",
				"ID", "Status", "Strategy", "Instrument", "Started")
			fmt.Fprintln(out, bar)
			for _, s := range statuses {
				started := s.StartedAt.Format("2006-01-02 15:04")
				fmt.Fprintf(out, "  %-12s %-12s %-16s %-10s %s\n",
					s.ID, s.Status, s.StrategyName, s.Instrument, started)
				if s.Error != "" {
					fmt.Fprintf(out, "    error: %s\n", s.Error)
				}
			}
			fmt.Fprintln(out, bar)
			return nil
		},
	}
}

// ── bot get ───────────────────────────────────────────────────────────────

func botGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show status of a single bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var s botsvc.BotStatus
			if err := apiGet(serverURL+"/api/v1/bots/"+args[0], &s); err != nil {
				return err
			}
			printBotDetail(cmd.OutOrStdout(), s)
			return nil
		},
	}
}

func printBotDetail(out io.Writer, s botsvc.BotStatus) {
	bar := strings.Repeat("─", 40)
	fmt.Fprintln(out, bar)
	fmt.Fprintf(out, "  %-20s %s\n", "ID", s.ID)
	fmt.Fprintln(out, bar)
	fmt.Fprintf(out, "  %-20s %s\n", "Status", s.Status)
	fmt.Fprintf(out, "  %-20s %s\n", "Instrument", s.Instrument)
	fmt.Fprintf(out, "  %-20s %s (%s)\n", "Strategy", s.StrategyName, s.StrategyKind)
	fmt.Fprintf(out, "  %-20s %.2f%%\n", "Risk", s.RiskPct)
	fmt.Fprintf(out, "  %-20s %s\n", "Tick Interval", s.TickInterval)
	fmt.Fprintf(out, "  %-20s %s\n", "Started", s.StartedAt.Format(time.RFC3339))
	if s.StoppedAt != nil {
		dur := s.StoppedAt.Sub(s.StartedAt).Round(time.Second)
		fmt.Fprintf(out, "  %-20s %s (ran %s)\n", "Stopped", s.StoppedAt.Format(time.RFC3339), dur)
	} else {
		dur := time.Since(s.StartedAt).Round(time.Second)
		fmt.Fprintf(out, "  %-20s running for %s\n", "Uptime", dur)
	}
	fmt.Fprintf(out, "  %-20s %d\n", "Ticks", s.Ticks)
	fmt.Fprintf(out, "  %-20s %d\n", "Opens", s.Opens)
	fmt.Fprintf(out, "  %-20s %d\n", "Closes", s.Closes)
	if s.Error != "" {
		fmt.Fprintf(out, "  %-20s %s\n", "Error", s.Error)
	}
	fmt.Fprintln(out, bar)
}

// ── bot report ────────────────────────────────────────────────────────────

func botReportCmd() *cobra.Command {
	var (
		all         bool
		journalPath string
	)
	cmd := &cobra.Command{
		Use:   "report <id> | --all",
		Short: "Show a detailed run report for one or all bots",
		Long: `Show a detailed report for a bot run, including metadata, runtime stats, and P&L.

Accepts a bot ID for a single report, or --all to report every known bot.
Works for both running and stopped bots.

P&L data is read from the journal file (--journal). Only trades tagged with a
bot ID (those opened after this feature was introduced) will appear in the P&L
section.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return fmt.Errorf("report --all does not accept a bot ID argument")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("report requires exactly one bot ID argument (or use --all)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// Read journal trades once; filter per-bot below.
			var allTrades []journal.TradeRecord
			if journalPath != "" {
				var err error
				allTrades, err = journal.ReadTradesJSONL(journalPath)
				if err != nil {
					fmt.Fprintf(out, "warning: could not read journal %s: %v\n\n", journalPath, err)
				}
			}

			printReport := func(s botsvc.BotStatus) {
				printBotDetail(out, s)
				printBotPL(out, s.ID, allTrades)
				fmt.Fprintln(out)
			}

			if all {
				var statuses []botsvc.BotStatus
				if err := apiGet(serverURL+"/api/v1/bots", &statuses); err != nil {
					return err
				}
				if len(statuses) == 0 {
					fmt.Fprintln(out, "No bots found.")
					return nil
				}
				for _, s := range statuses {
					printReport(s)
				}
				return nil
			}
			var s botsvc.BotStatus
			if err := apiGet(serverURL+"/api/v1/bots/"+args[0], &s); err != nil {
				return err
			}
			printReport(s)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Report all bots (running and stopped)")
	cmd.Flags().StringVar(&journalPath, "journal", "./live-trades.jsonl", "Path to JSONL trades journal for P&L data")
	return cmd
}

func printBotPL(out io.Writer, botID string, trades []journal.TradeRecord) {
	var botTrades []journal.TradeRecord
	for _, t := range trades {
		if t.BotID == botID {
			botTrades = append(botTrades, t)
		}
	}
	if len(botTrades) == 0 {
		return
	}
	bar := strings.Repeat("─", 40)
	fmt.Fprintln(out, bar)
	fmt.Fprintf(out, "  %-20s %d trades\n", "Closed Trades", len(botTrades))
	var totalPL float64
	wins, losses := 0, 0
	for _, t := range botTrades {
		pl := t.RealizedPL.Float64()
		totalPL += pl
		if pl >= 0 {
			wins++
		} else {
			losses++
		}
		fmt.Fprintf(out, "    %-8s  entry=%.5f  exit=%.5f  pl=%+.2f  %s\n",
			t.TradeID,
			t.EntryPrice.Float64(),
			t.ExitPrice.Float64(),
			pl,
			t.Reason,
		)
	}
	fmt.Fprintf(out, "  %-20s %+.2f\n", "Total P&L", totalPL)
	fmt.Fprintf(out, "  %-20s %d W / %d L\n", "Win/Loss", wins, losses)
	fmt.Fprintln(out, bar)
}

// ── bot start ─────────────────────────────────────────────────────────────

func botStartCmd(rc *config.RootConfig) *cobra.Command {
	var (
		configFile   string
		instrument   string
		stratName    string
		tickInterval string
		riskPct      float64
		maxUnits     int64
		// Local mode flags — only used when --local is set.
		local     bool
		token     string
		accountID string
		env       string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new live strategy bot",
		Long: `Start one or more live strategy bots.

Server mode (default): the bot runs inside a trader serve process.
  trader bot start --instrument EUR_USD --strategy pulse
  trader bot start --config testdata/configs/smoke-test.yml

Local mode (--local): runs directly in this process, no server required.
  trader bot start --local --instrument EUR_USD --strategy pulse

In local mode the process blocks until Ctrl-C, then stops the bot cleanly.
OANDA positions are NOT closed on stop — they remain on the broker.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := botsvc.BotConfig{
				Instrument:   instrument,
				TickInterval: tickInterval,
				RiskPct:      riskPct,
				MaxUnits:     maxUnits,
				Strategy:     botsvc.StrategyConfig{Kind: stratName},
			}

			if local {
				return startLocal(cmd, rc, cfg, configFile, token, accountID, env)
			}

			if configFile != "" {
				return startFromConfig(cmd, configFile)
			}
			if instrument == "" || stratName == "" {
				return fmt.Errorf("--instrument and --strategy are required (or use --config)")
			}
			return startOne(cmd, cfg)
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Portfolio YAML config — starts one bot per instrument")
	cmd.Flags().StringVar(&instrument, "instrument", "", "Instrument, e.g. EUR_USD")
	cmd.Flags().StringVar(&stratName, "strategy", "", "Strategy name, e.g. donchian-v6, pulse")
	cmd.Flags().StringVar(&tickInterval, "tick-interval", "60s", "How often the strategy ticks, e.g. 30s, 5m")
	cmd.Flags().Float64Var(&riskPct, "risk-pct", 1.0, "Percent of account equity to risk per trade")
	cmd.Flags().Int64Var(&maxUnits, "max-units", 0, "Maximum position size in units (0 = no limit)")
	// Local mode flags.
	cmd.Flags().BoolVar(&local, "local", false, "Run bot directly in this process (no trader serve required)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (local mode)")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (local mode)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live (local mode)")
	return cmd
}

// startLocal runs a single bot directly in the current process without a
// trader serve daemon. It blocks until the bot exits or Ctrl-C is received,
// then stops the bot and waits for it to finish cleanly.
func startLocal(cmd *cobra.Command, rc *config.RootConfig, cfg botsvc.BotConfig, configFile, token, accountID, env string) error {
	if configFile != "" {
		return fmt.Errorf("--config is not supported with --local; start bots one at a time")
	}
	if cfg.Instrument == "" || cfg.Strategy.Kind == "" {
		return fmt.Errorf("--instrument and --strategy are required in local mode")
	}

	// Token/account: explicit flag > global config > env var (same precedence as live run).
	tok := token
	if !cmd.Flags().Changed("token") {
		if rc.OANDA.Token != "" {
			tok = rc.OANDA.Token
		}
	}
	resolvedAccount := accountID
	if !cmd.Flags().Changed("account-id") {
		if rc.OANDA.AccountID != "" {
			resolvedAccount = rc.OANDA.AccountID
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client, err := oanda.NewClient(env, tok)
	if err != nil {
		return err
	}
	resolvedID, err := accountsvc.ResolveAccountID(ctx, client, resolvedAccount)
	if err != nil {
		var amb accountsvc.AmbiguousAccountError
		if errors.As(err, &amb) {
			fmt.Fprintln(cmd.ErrOrStderr(), "Multiple accounts — specify one with --account-id:")
			for _, id := range amb.Accounts {
				fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", id)
			}
		}
		return err
	}

	// Prevent two local bots from trading the same account simultaneously.
	_, release, err := acquireAccountLock(resolvedID)
	if err != nil {
		return err
	}
	defer release()

	acc, err := accountsvc.Resolve(ctx, resolvedID, client, log.L)
	if err != nil {
		return fmt.Errorf("start bot: %w", err)
	}
	status, err := botsvc.StartBotOnAccount(ctx, acc, cfg, client, log.L)
	if err != nil {
		return fmt.Errorf("start bot: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Bot started: %s | %s | %s | tick=%s\nPress Ctrl-C to stop.\n",
		status.ID, status.StrategyName, status.Instrument, status.TickInterval)

	// Block until Ctrl-C cancels the context, then stop the bot and wait.
	<-ctx.Done()
	fmt.Fprintf(cmd.OutOrStdout(), "\nStopping bot %s...\n", status.ID)
	_ = botsvc.StopBot(status.ID)

	if err := botsvc.WaitBot(status.ID); err != nil {
		return fmt.Errorf("bot exited with error: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Bot %s stopped.\n", status.ID)
	return nil
}

// startFromConfig reads a portfolio YAML and starts one bot per instrument.
// It attempts every instrument and returns an error if any of them failed.
func startFromConfig(cmd *cobra.Command, configFile string) error {
	cfg, err := botsvc.LoadPortfolioConfig(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	out := cmd.OutOrStdout()
	var failed []string
	for _, inst := range cfg.Instruments {
		riskPct := inst.RiskPct
		if riskPct <= 0 {
			riskPct = cfg.RiskPct
		}
		tick := inst.TickInterval
		if tick == "" {
			tick = "60s"
		}
		localWarmup := inst.LocalWarmupBars
		if localWarmup <= 0 {
			localWarmup = cfg.LocalWarmupBars
		}
		regimeCfg := strategy.RegimeConfig{Kind: inst.Regime.Kind, Params: inst.Regime.Params}
		for _, f := range inst.Regime.Filters {
			regimeCfg.Filters = append(regimeCfg.Filters, strategy.RegimeConfig{Kind: f.Kind, Params: f.Params})
		}
		bc := botsvc.BotConfig{
			Instrument:   inst.Instrument,
			TickInterval: tick,
			RiskPct:      riskPct,
			MaxUnits:     inst.MaxUnits,
			Strategy: botsvc.StrategyConfig{
				Kind:            inst.Strategy.Kind,
				Granularity:     inst.Timeframe,
				Params:          inst.Strategy.Params,
				Exit:            strategy.ExitConfig{Kind: inst.Exit.Kind, Params: inst.Exit.Params},
				Regime:          regimeCfg,
				WarmupBars:      inst.WarmupBars,
				LocalWarmupBars: localWarmup,
			},
		}
		if err := startOne(cmd, bc); err != nil {
			fmt.Fprintf(out, "  ERROR starting %s: %v\n", inst.Instrument, err)
			failed = append(failed, inst.Instrument)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to start bots for: %s", strings.Join(failed, ", "))
	}
	return nil
}

func startOne(cmd *cobra.Command, cfg botsvc.BotConfig) error {
	var status botsvc.BotStatus
	if err := apiPost(serverURL+"/api/v1/bots", cfg, &status); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Bot started: %-12s %-16s %-10s %s\n",
		status.ID, status.StrategyName, status.Instrument, status.Status)
	return nil
}

// ── bot stop ──────────────────────────────────────────────────────────────

func botStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop a running bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := apiDelete(serverURL + "/api/v1/bots/" + args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Bot %s stopped.\n", args[0])
			return nil
		},
	}
}

// ── HTTP helpers ──────────────────────────────────────────────────────────

func apiGet(url string, out any) error {
	resp, err := http.Get(url) //nolint:gosec // URL is controlled by the user via --server flag
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func apiPost(url string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b)) //nolint:gosec
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func apiDelete(url string) error {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("build DELETE %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func decodeResponse(resp *http.Response, out any) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		// Try to extract the "error" field from a JSON error body.
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errBody) == nil && errBody.Error != "" {
			return fmt.Errorf("server error %d: %s", resp.StatusCode, errBody.Error)
		}
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}
