package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type RootConfig struct {
	ConfigPath string
	GlobalPath string
	DBPath     string
	LogLevel   string
	NoColor    bool

	// Paths    PathsConfig
	// Defaults DefaultsConfig
	// Backtest BacktestConfig
}

// appConfig represents the complete simulation configuration.
type appConfig struct {
	Root       RootConfig        `json:"root" yaml:"root"`
	Account    accountConfig     `json:"account" yaml:"account"`
	Strategy   appStrategyConfig `json:"strategy" yaml:"strategy"`
	Simulation simulationConfig  `json:"simulation" yaml:"simulation"`
	Journal    journalConfig     `json:"journal" yaml:"journal"`
	Replay     *replayConfig     `json:"replay,omitempty" yaml:"replay,omitempty"`
}

// accountConfig contains account initialization parameters
type accountConfig struct {
	ID       string  `json:"id" yaml:"id"`
	Currency string  `json:"currency" yaml:"currency"`
	Balance  float64 `json:"balance" yaml:"balance"`
}

// appStrategyConfig contains strategy parameters.
type appStrategyConfig struct {
	RiskPercent float64 `json:"risk_percent" yaml:"risk_percent"`
	Instrument  string  `json:"instrument" yaml:"instrument"`
	StopPips    float64 `json:"stop_pips" yaml:"stop_pips"`
	TargetPips  float64 `json:"target_pips" yaml:"target_pips"`
}

// simulationConfig contains simulation parameters
type simulationConfig struct {
	InitialBid float64     `json:"initial_bid" yaml:"initial_bid"`
	InitialAsk float64     `json:"initial_ask" yaml:"initial_ask"`
	PriceSteps []priceStep `json:"price_steps,omitempty" yaml:"price_steps,omitempty"`
}

// priceStep represents a price update in the simulation
type priceStep struct {
	Bid   float64 `json:"bid" yaml:"bid"`
	Ask   float64 `json:"ask" yaml:"ask"`
	Delay string  `json:"delay" yaml:"delay"` // e.g., "1h", "30m", "1s"
}

// ParseDuration converts the delay string to time.Duration
func (ps priceStep) ParseDuration() (time.Duration, error) {
	if ps.Delay == "" {
		return 0, nil
	}
	return time.ParseDuration(ps.Delay)
}

// journalConfig contains journaling parameters
type journalConfig struct {
	Type       string `json:"type" yaml:"type"` // "csv" or "sqlite"
	TradesFile string `json:"trades_file,omitempty" yaml:"trades_file,omitempty"`
	EquityFile string `json:"equity_file,omitempty" yaml:"equity_file,omitempty"`
	DBPath     string `json:"db_path,omitempty" yaml:"db_path,omitempty"`
}

// replayConfig contains replay-specific parameters
type replayConfig struct {
	CSVFile       string `json:"csv_file" yaml:"csv_file"`               // Path to CSV file with tick data
	TickThenEvent bool   `json:"tick_then_event" yaml:"tick_then_event"` // Process tick before event
	CloseAtEnd    bool   `json:"close_at_end" yaml:"close_at_end"`       // Close all trades at end of replay
}

// loadFromFile loads configuration from a file (JSON or YAML based on extension)
func loadFromFile(path string) (*appConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &appConfig{}

	// Try YAML first, fall back to JSON
	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		err = json.Unmarshal(data, cfg)
		if err != nil {
			return nil, fmt.Errorf("parse config (tried YAML and JSON): %w", err)
		}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// SaveToFile saves configuration to a file (JSON or YAML based on extension)
func (c *appConfig) SaveToFile(path string) error {
	var data []byte
	var err error

	// Determine format by extension
	if (len(path) > 5 && path[len(path)-5:] == ".yaml") || (len(path) > 4 && path[len(path)-4:] == ".yml") {
		data, err = yaml.Marshal(c)
	} else {
		data, err = json.MarshalIndent(c, "", "  ")
	}

	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// Validate checks if the configuration is valid
func (c *appConfig) Validate() error {
	if c.Account.Currency == "" {
		return fmt.Errorf("account.currency is required")
	}
	if c.Account.Balance <= 0 {
		return fmt.Errorf("account.balance must be positive")
	}
	if c.Strategy.RiskPercent <= 0 || c.Strategy.RiskPercent > 1 {
		return fmt.Errorf("strategy.risk_percent must be between 0 and 1")
	}
	if c.Strategy.Instrument == "" {
		return fmt.Errorf("strategy.instrument is required")
	}
	// Validate that the instrument exists in the market
	if _, ok := Instruments[c.Strategy.Instrument]; !ok {
		return fmt.Errorf("unknown instrument: %s", c.Strategy.Instrument)
	}
	if c.Strategy.StopPips <= 0 {
		return fmt.Errorf("strategy.stop_pips must be positive")
	}
	if c.Strategy.TargetPips <= 0 {
		return fmt.Errorf("strategy.target_pips must be positive")
	}
	if c.Simulation.InitialBid <= 0 || c.Simulation.InitialAsk <= 0 {
		return fmt.Errorf("simulation initial prices must be positive")
	}
	if c.Simulation.InitialAsk <= c.Simulation.InitialBid {
		return fmt.Errorf("simulation initial_ask must be greater than initial_bid")
	}
	if c.Journal.Type != "csv" && c.Journal.Type != "sqlite" {
		return fmt.Errorf("journal.type must be 'csv' or 'sqlite'")
	}
	if c.Journal.Type == "csv" && (c.Journal.TradesFile == "" || c.Journal.EquityFile == "") {
		return fmt.Errorf("journal trades_file and equity_file required for CSV type")
	}
	if c.Journal.Type == "sqlite" && c.Journal.DBPath == "" {
		return fmt.Errorf("journal db_path required for SQLite type")
	}
	return nil
}

// defaultConfig returns a configuration with sensible defaults
func defaultConfig() *appConfig {
	return &appConfig{
		Account: accountConfig{
			ID:       "SIM-001",
			Currency: "USD",
			Balance:  100000,
		},
		Strategy: appStrategyConfig{
			RiskPercent: 0.01,
			Instrument:  "EURUSD",
			StopPips:    20,
			TargetPips:  40,
		},
		Simulation: simulationConfig{
			InitialBid: 1.0849,
			InitialAsk: 1.0851,
		},
		Journal: journalConfig{
			Type:       "csv",
			TradesFile: "./trades.csv",
			EquityFile: "./equity.csv",
		},
	}
}
