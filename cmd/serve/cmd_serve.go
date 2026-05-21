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
	"github.com/rustyeddy/trader/api/rest"
	"github.com/rustyeddy/trader/service"
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
		Level string `yaml:"level"`
	} `yaml:"log"`
}

// New returns the "serve" cobra command.
func New(rc *trader.RootConfig) *cobra.Command {
	var (
		cfgFile   string
		addr      string
		token     string
		accountID string
		env       string
		logLevel  string
		journalDB string
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
  5. REST API server (:8080 by default)
  6. Graceful shutdown on SIGTERM / SIGINT

Configuration can be loaded from a YAML file (--config) with CLI flags
taking precedence over file values.

Example config file (see deploy/trader.yaml.example):
  env: practice
  token: ""          # or set OANDA_TOKEN
  account_id: ""     # auto-discovered if omitted
  rest:
    addr: ":8080"
  journal:
    kind: sqlite
    sqlite_path: /var/lib/trader/journal.db
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
			if token != "" {
				cfg.Token = token
			}
			if accountID != "" {
				cfg.AccountID = accountID
			}
			if env != "" && env != "practice" {
				cfg.Env = env
			}
			if addr != "" && addr != ":8080" {
				cfg.REST.Addr = addr
			}
			if logLevel != "" && logLevel != "info" {
				cfg.Log.Level = logLevel
			}
			if journalDB != "" {
				cfg.Journal.Kind = "sqlite"
				cfg.Journal.SQLitePath = journalDB
			}

			// Apply defaults.
			if cfg.Env == "" {
				cfg.Env = "practice"
			}
			if cfg.REST.Addr == "" {
				cfg.REST.Addr = ":8080"
			}
			if cfg.Journal.Kind == "" {
				cfg.Journal.Kind = "sqlite"
			}
			if cfg.Journal.SQLitePath == "" {
				cfg.Journal.SQLitePath = "./trader.db"
			}
			if cfg.Log.Level == "" {
				cfg.Log.Level = "info"
			}
			if cfg.Data.Dir != "" {
				trader.SetDataDir(cfg.Data.Dir)
			}

			// Resolve token.
			tok := cfg.Token
			if tok == "" {
				tok = os.Getenv("OANDA_TOKEN")
			}
			if tok == "" {
				tok = readTokenFile()
			}

			if err := trader.Setup(trader.LogConfig{
				Level:  cfg.Log.Level,
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

			// REST API.
			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Info("serve: REST API starting", "addr", cfg.REST.Addr)
				srv := rest.New(svc, cfg.REST.Addr)
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
	cmd.Flags().StringVar(&addr, "addr", ":8080", "REST API listen address")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	cmd.Flags().StringVar(&journalDB, "journal-db", "", "SQLite journal path (default ./trader.db)")

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
