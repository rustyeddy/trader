// Package serve implements "trader serve" — the long-running daemon mode.
// It boots the REST API server and the live-journal transaction stream
// subscription side-by-side, handles signals, and reconnects on broker
// disconnect with exponential backoff.
package serve

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader"
	mcpserver "github.com/rustyeddy/trader/api/mcp"
	"github.com/rustyeddy/trader/api/rest"
	"github.com/rustyeddy/trader/service"
	traderui "github.com/rustyeddy/trader/ui"
)

// DaemonConfig is the full config file schema for trader serve.
// Flags on the CLI override the same field from the file.
type DaemonConfig struct {
	// OANDA credentials.
	Env       string `yaml:"env"`
	Token     string `yaml:"token"`
	AccountID string `yaml:"account_id"`

	REST struct {
		Addr string `yaml:"addr"`
	} `yaml:"rest"`

	Journal service.JournalConfig `yaml:"journal"`

	Data struct {
		Dir string `yaml:"dir"`
	} `yaml:"data"`

	Log struct {
		Level  string `yaml:"level"`
		File   string `yaml:"file"`   // path to log file; empty = stdout only
		Format string `yaml:"format"` // "json" or "text" (default)
	} `yaml:"log"`
}

// New returns the "serve" cobra command.
func New(rc *trader.RootConfig) *cobra.Command {
	var (
		cfgFile        string
		addr           string
		token          string
		accountID      string
		env            string
		journalTrades  string
		journalEquity  string
		reportsDir     string
		mcpEnableWrite bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run trader as a long-running daemon (REST API + live journal)",
		Long: `Start trader in daemon mode.

Boots:
  1. Structured logging
  2. DataManager with warm candle cache
  3. OANDA broker connection
  4. Transaction stream → journal writer (reconnects on disconnect)
  5. REST API server (:9999 by default)
  6. Graceful shutdown on SIGTERM / SIGINT

Configuration can be loaded from a YAML file (--config) with CLI flags
taking precedence over file values.

Example config file (see deploy/trader.yaml.example):
  env: practice
  token: ""          # or set OANDA_TOKEN
  account_id: ""     # auto-discovered if omitted
  rest:
    addr: ":9999"
  journal:
    kind: json
    trades_path: /var/lib/trader/live-trades.jsonl
    equity_path: /var/lib/trader/live-equity.jsonl
  log:
    level: info
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			cfg, err := loadConfig(cfgFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// CLI flags override config file.
			if cmd.Flags().Changed("token") {
				cfg.Token = token
			}
			if cmd.Flags().Changed("account-id") {
				cfg.AccountID = accountID
			}
			if env != "" && env != "practice" {
				cfg.Env = env
			}
			if addr != "" && addr != ":9999" {
				cfg.REST.Addr = addr
			}
			if cmd.Flags().Changed("log-level") {
				cfg.Log.Level = rc.LogLevel
			}
			if cmd.Flags().Changed("log-file") {
				cfg.Log.File = rc.LogFile
			}
			if cmd.Flags().Changed("log-format") {
				cfg.Log.Format = rc.LogFormat
			}
			if journalTrades != "" {
				cfg.Journal.Kind = "json"
				cfg.Journal.TradesPath = journalTrades
			}
			if journalEquity != "" {
				cfg.Journal.Kind = "json"
				cfg.Journal.EquityPath = journalEquity
			}

			// Apply defaults.
			if cfg.Env == "" {
				cfg.Env = "practice"
			}
			if cfg.REST.Addr == "" {
				cfg.REST.Addr = ":9999"
			}
			if cfg.Journal.Kind == "" {
				cfg.Journal.Kind = "json"
			}
			if cfg.Journal.TradesPath == "" {
				cfg.Journal.TradesPath = "./live-trades.jsonl"
			}
			if cfg.Journal.EquityPath == "" {
				cfg.Journal.EquityPath = "./live-equity.jsonl"
			}
			if cfg.Log.Level == "" {
				cfg.Log.Level = "info"
			}
			if cfg.Data.Dir != "" {
				trader.SetDataDir(cfg.Data.Dir)
			}

			// Resolve token: YAML/flag > global config > env var > token file.
			tok := cfg.Token
			if tok == "" {
				tok = rc.OANDAToken
			}
			if tok == "" {
				tok = os.Getenv("OANDA_TOKEN")
			}
			if tok == "" {
				tok = readTokenFile()
			}

			// Resolve account: YAML/flag > global config > env var.
			if cfg.AccountID == "" {
				cfg.AccountID = rc.OANDAAccountID
			}
			if cfg.AccountID == "" {
				cfg.AccountID = os.Getenv("OANDA_ACCOUNT_ID")
			}

			if err := trader.Setup(trader.LogConfig{
				Level:  cfg.Log.Level,
				File:   cfg.Log.File,
				Format: cfg.Log.Format,
				Stdout: true,
			}); err != nil {
				return err
			}
			log := trader.L

			if tok == "" {
				log.Warn("serve: no OANDA token — live trading and journal disabled")
			}

			// Build service.
			var svc *service.Service
			if tok != "" {
				svc, err = service.New(service.Config{
					Env:       cfg.Env,
					Token:     tok,
					AccountID: cfg.AccountID,
					Log:       log,
				})
				if err != nil {
					return fmt.Errorf("init service: %w", err)
				}
				log.Info("serve: OANDA service ready", "env", cfg.Env)
			} else {
				svc = &service.Service{Log: log}
			}

			var wg sync.WaitGroup
			errs := make(chan error, 2)

			// REST API — with embedded UI assets and MCP.
			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Info("serve: REST API starting", "addr", cfg.REST.Addr)
				srv := rest.New(svc, cfg.REST.Addr)
				if reportsDir != "" {
					srv.WithReportsDir(reportsDir)
					log.Info("serve: reports dir", "path", reportsDir)
				}
				// MCP over HTTP at POST /mcp (read-only by default).
				mcpSrv := mcpserver.New(svc, mcpEnableWrite)
				if reportsDir != "" {
					mcpSrv.WithReportsDir(reportsDir)
				}
				srv.WithMCPHandler(mcpSrv.HTTPHandler())
				log.Info("serve: MCP available", "endpoint", "POST /mcp", "write", mcpEnableWrite)
				if mcpEnableWrite {
					log.Warn("serve: MCP write tools enabled over HTTP — POST /mcp has no authentication; " +
						"restrict network access or add a reverse-proxy auth layer before exposing to untrusted origins")
				}
				// Serve the SvelteKit UI from the embedded dist/ directory.
				uiFS, fsErr := traderui.SubFS()
				if fsErr == nil {
					srv.WithStatic(uiFS)
					log.Info("serve: UI assets embedded")
				} else {
					log.Warn("serve: UI assets not available", "err", fsErr)
				}
				if err := srv.Serve(ctx); err != nil {
					errs <- fmt.Errorf("REST: %w", err)
				}
			}()

			// Live journal subscription (only if OANDA is available).
			if svc.OANDA != nil {
				if err := svc.ResolveAccount(ctx); err != nil {
					log.Warn("serve: account resolve failed; journal disabled", "err", err)
				} else {
					wg.Add(1)
					go func() {
						defer wg.Done()
						runJournalWithBackoff(ctx, svc, cfg.Journal, log)
					}()
				}
			}

			fmt.Printf("trader daemon running — REST on %s — Ctrl-C to exit\n", cfg.REST.Addr)

			// Wait for a fatal error or context cancellation.
			select {
			case err := <-errs:
				cancel()
				wg.Wait()
				return err
			case <-ctx.Done():
				log.Info("serve: shutting down...")
				wg.Wait()
				log.Info("serve: stopped")
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "Path to YAML config file")
	cmd.Flags().StringVar(&addr, "addr", ":9999", "REST API listen address")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&journalTrades, "journal-trades", "", "Journal trade-record path (default ./live-trades.jsonl)")
	cmd.Flags().StringVar(&journalEquity, "journal-equity", "", "Journal equity-record path (default ./live-equity.jsonl)")
	cmd.Flags().StringVar(&reportsDir, "reports-dir", "", "Backtest reports directory (default /srv/trading/backtests/reports)")
	cmd.Flags().BoolVar(&mcpEnableWrite, "mcp-enable-write", false, "Enable MCP write tools (place_order, close_trade, update_stop) over HTTP")

	return cmd
}

// runJournalWithBackoff runs RunLiveJournal, restarting after disconnect
// with exponential backoff (cap 5 min). Stops when ctx is cancelled.
func runJournalWithBackoff(ctx context.Context, svc *service.Service, jcfg service.JournalConfig, log *slog.Logger) {
	const (
		baseDelay = 5 * time.Second
		maxDelay  = 5 * time.Minute
	)
	attempt := 0

	for {
		journal, err := svc.OpenJournal(jcfg)
		if err != nil {
			log.Error("serve: open journal failed", "err", err)
		} else {
			log.Info("serve: live journal starting", "kind", jcfg.Kind)
			lastID, err := svc.RunLiveJournal(ctx, journal, 0)
			journal.Close()
			if ctx.Err() != nil {
				return // clean shutdown
			}
			log.Warn("serve: journal stream ended", "last_tx_id", lastID, "err", err)
		}

		attempt++
		delay := time.Duration(math.Min(
			float64(baseDelay)*math.Pow(2, float64(attempt-1)),
			float64(maxDelay),
		))
		log.Info("serve: reconnecting journal", "attempt", attempt, "in", delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// loadConfig reads a YAML config file. Returns a zero-value config on
// empty path (all values come from flags / env).
func loadConfig(path string) (DaemonConfig, error) {
	var cfg DaemonConfig
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func readTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
