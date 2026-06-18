// Package bot hosts CLI subcommands for managing live strategy bots running
// inside a trader serve process. All commands are thin HTTP clients that talk
// to the REST API — bots run as goroutines inside the server and cannot be
// managed directly from a separate process.
package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

var serverURL string

func New(rc *traderpkg.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bot",
		Short: "Manage live strategy bots running inside trader serve",
	}
	cmd.PersistentFlags().StringVar(&serverURL, "server", defaultServer(), "trader serve base URL")
	cmd.AddCommand(
		botListCmd(),
		botGetCmd(),
		botStartCmd(),
		botStopCmd(),
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
			var statuses []service.BotStatus
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
			var s service.BotStatus
			if err := apiGet(serverURL+"/api/v1/bots/"+args[0], &s); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			bar := strings.Repeat("─", 36)
			fmt.Fprintln(out, bar)
			fmt.Fprintf(out, "  %-18s %s\n", "ID", s.ID)
			fmt.Fprintln(out, bar)
			fmt.Fprintf(out, "  %-18s %s\n", "Status", s.Status)
			fmt.Fprintf(out, "  %-18s %s\n", "Strategy", s.StrategyName)
			fmt.Fprintf(out, "  %-18s %s\n", "Instrument", s.Instrument)
			fmt.Fprintf(out, "  %-18s %s\n", "Started", s.StartedAt.Format(time.RFC3339))
			if s.Error != "" {
				fmt.Fprintf(out, "  %-18s %s\n", "Error", s.Error)
			}
			fmt.Fprintln(out, bar)
			return nil
		},
	}
}

// ── bot start ─────────────────────────────────────────────────────────────

func botStartCmd() *cobra.Command {
	var (
		configFile   string
		instrument   string
		stratName    string
		tickInterval string
		riskPct      float64
		maxUnits     int64
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new live strategy bot on the server",
		Long: `Start one or more live strategy bots inside the running trader serve process.

Single bot (flags):
  trader bot start --instrument EUR_USD --strategy pulse

From a portfolio config (starts one bot per instrument):
  trader bot start --config testdata/configs/smoke-test.yml

Bots run until stopped with "bot stop <id>" or the server restarts.
OANDA positions are NOT closed on stop — they remain on the broker.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configFile != "" {
				return startFromConfig(cmd, configFile)
			}
			// Single-bot path.
			if instrument == "" || stratName == "" {
				return fmt.Errorf("--instrument and --strategy are required (or use --config)")
			}
			return startOne(cmd, service.BotConfig{
				Instrument:   instrument,
				TickInterval: tickInterval,
				RiskPct:      riskPct,
				MaxUnits:     maxUnits,
				Strategy:     service.StrategyConfig{Kind: stratName},
			})
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Portfolio YAML config — starts one bot per instrument")
	cmd.Flags().StringVar(&instrument, "instrument", "", "Instrument, e.g. EUR_USD")
	cmd.Flags().StringVar(&stratName, "strategy", "", "Strategy name, e.g. donchian-v6, pulse")
	cmd.Flags().StringVar(&tickInterval, "tick-interval", "60s", "How often the strategy ticks, e.g. 30s, 5m")
	cmd.Flags().Float64Var(&riskPct, "risk-pct", 1.0, "Percent of account equity to risk per trade")
	cmd.Flags().Int64Var(&maxUnits, "max-units", 0, "Maximum position size in units (0 = no limit)")
	return cmd
}

// startFromConfig reads a portfolio YAML and starts one bot per instrument.
// It attempts every instrument and returns an error if any of them failed.
func startFromConfig(cmd *cobra.Command, configFile string) error {
	cfg, err := service.LoadPortfolioConfig(configFile)
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
		bc := service.BotConfig{
			Instrument:   inst.Instrument,
			TickInterval: tick,
			RiskPct:      riskPct,
			MaxUnits:     inst.MaxUnits,
			Strategy: service.StrategyConfig{
				Kind:   inst.Strategy.Kind,
				Params: inst.Strategy.Params,
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

func startOne(cmd *cobra.Command, cfg service.BotConfig) error {
	var status service.BotStatus
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
