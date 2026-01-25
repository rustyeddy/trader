package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rustyeddy/trader/market"
	"gopkg.in/yaml.v3"
)

// Config represents the complete simulation configuration
type Config struct {
	Account    AccountConfig    `json:"account" yaml:"account"`
	Strategy   StrategyConfig   `json:"strategy" yaml:"strategy"`
	Simulation SimulationConfig `json:"simulation" yaml:"simulation"`
	Journal    JournalConfig    `json:"journal" yaml:"journal"`
}

// AccountConfig contains account initialization parameters
type AccountConfig struct {
	ID       string  `json:"id" yaml:"id"`
	Currency string  `json:"currency" yaml:"currency"`
	Balance  float64 `json:"balance" yaml:"balance"`
}

// StrategyConfig contains strategy parameters
type StrategyConfig struct {
	RiskPercent float64 `json:"risk_percent" yaml:"risk_percent"`
	Instrument  string  `json:"instrument" yaml:"instrument"`
	StopPips    float64 `json:"stop_pips" yaml:"stop_pips"`
	TargetPips  float64 `json:"target_pips" yaml:"target_pips"`
}

// SimulationConfig contains simulation parameters
type SimulationConfig struct {
	InitialBid  float64   `json:"initial_bid" yaml:"initial_bid"`
	InitialAsk  float64   `json:"initial_ask" yaml:"initial_ask"`
	PriceSteps  []PriceStep `json:"price_steps,omitempty" yaml:"price_steps,omitempty"`
}

// PriceStep represents a price update in the simulation
type PriceStep struct {
	Bid   float64 `json:"bid" yaml:"bid"`
	Ask   float64 `json:"ask" yaml:"ask"`
	Delay string  `json:"delay" yaml:"delay"` // e.g., "1h", "30m", "1s"
}

// ParseDuration converts the delay string to time.Duration
func (ps PriceStep) ParseDuration() (time.Duration, error) {
	if ps.Delay == "" {
		return 0, nil
	}
	return time.ParseDuration(ps.Delay)
}

// JournalConfig contains journaling parameters
type JournalConfig struct {
	Type       string `json:"type" yaml:"type"` // "csv" or "sqlite"
	TradesFile string `json:"trades_file,omitempty" yaml:"trades_file,omitempty"`
	EquityFile string `json:"equity_file,omitempty" yaml:"equity_file,omitempty"`
	DBPath     string `json:"db_path,omitempty" yaml:"db_path,omitempty"`
}

// LoadFromFile loads configuration from a file (JSON or YAML based on extension)
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}

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
func (c *Config) SaveToFile(path string) error {
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
func (c *Config) Validate() error {
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
	if _, ok := market.Instruments[c.Strategy.Instrument]; !ok {
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

// Default returns a configuration with sensible defaults
func Default() *Config {
	return &Config{
		Account: AccountConfig{
			ID:       "SIM-001",
			Currency: "USD",
			Balance:  100000,
		},
		Strategy: StrategyConfig{
			RiskPercent: 0.01,
			Instrument:  "EUR_USD",
			StopPips:    20,
			TargetPips:  40,
		},
		Simulation: SimulationConfig{
			InitialBid: 1.0849,
			InitialAsk: 1.0851,
		},
		Journal: JournalConfig{
			Type:       "csv",
			TradesFile: "./trades.csv",
			EquityFile: "./equity.csv",
		},
	}
}
