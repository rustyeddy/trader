package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	assert.NotNil(t, cfg)
	assert.Equal(t, "USD", cfg.Account.Currency)
	assert.Equal(t, 100000.0, cfg.Account.Balance)
	assert.Equal(t, 0.01, cfg.Strategy.RiskPercent)
	assert.NoError(t, cfg.Validate())
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			config:  Default(),
			wantErr: false,
		},
		{
			name: "missing currency",
			config: &Config{
				Account: AccountConfig{Balance: 100000},
			},
			wantErr: true,
			errMsg:  "account.currency is required",
		},
		{
			name: "negative balance",
			config: &Config{
				Account: AccountConfig{Currency: "USD", Balance: -1000},
			},
			wantErr: true,
			errMsg:  "account.balance must be positive",
		},
		{
			name: "invalid risk percent",
			config: &Config{
				Account:  AccountConfig{Currency: "USD", Balance: 100000},
				Strategy: StrategyConfig{RiskPercent: 1.5},
			},
			wantErr: true,
			errMsg:  "strategy.risk_percent must be between 0 and 1",
		},
		{
			name: "unknown instrument",
			config: &Config{
				Account:    AccountConfig{Currency: "USD", Balance: 100000},
				Strategy:   StrategyConfig{RiskPercent: 0.01, Instrument: "INVALID", StopPips: 20, TargetPips: 40},
				Simulation: SimulationConfig{InitialBid: 1.0849, InitialAsk: 1.0851},
				Journal:    JournalConfig{Type: "csv", TradesFile: "trades.csv", EquityFile: "equity.csv"},
			},
			wantErr: true,
			errMsg:  "unknown instrument",
		},
		{
			name: "negative stop pips",
			config: &Config{
				Account:    AccountConfig{Currency: "USD", Balance: 100000},
				Strategy:   StrategyConfig{RiskPercent: 0.01, Instrument: "EUR_USD", StopPips: -10, TargetPips: 40},
				Simulation: SimulationConfig{InitialBid: 1.0849, InitialAsk: 1.0851},
				Journal:    JournalConfig{Type: "csv", TradesFile: "trades.csv", EquityFile: "equity.csv"},
			},
			wantErr: true,
			errMsg:  "strategy.stop_pips must be positive",
		},
		{
			name: "zero target pips",
			config: &Config{
				Account:    AccountConfig{Currency: "USD", Balance: 100000},
				Strategy:   StrategyConfig{RiskPercent: 0.01, Instrument: "EUR_USD", StopPips: 20, TargetPips: 0},
				Simulation: SimulationConfig{InitialBid: 1.0849, InitialAsk: 1.0851},
				Journal:    JournalConfig{Type: "csv", TradesFile: "trades.csv", EquityFile: "equity.csv"},
			},
			wantErr: true,
			errMsg:  "strategy.target_pips must be positive",
		},
		{
			name: "ask <= bid",
			config: &Config{
				Account:    AccountConfig{Currency: "USD", Balance: 100000},
				Strategy:   StrategyConfig{RiskPercent: 0.01, Instrument: "EUR_USD", StopPips: 20, TargetPips: 40},
				Simulation: SimulationConfig{InitialBid: 1.0850, InitialAsk: 1.0849},
				Journal:    JournalConfig{Type: "csv", TradesFile: "trades.csv", EquityFile: "equity.csv"},
			},
			wantErr: true,
			errMsg:  "simulation initial_ask must be greater than initial_bid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name string
		ext  string
	}{
		{"json format", ".json"},
		{"yaml format", ".yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			path := filepath.Join(tmpDir, "test"+tt.ext)

			// Save
			err := cfg.SaveToFile(path)
			require.NoError(t, err)

			// Verify file exists
			_, err = os.Stat(path)
			require.NoError(t, err)

			// Load
			loaded, err := LoadFromFile(path)
			require.NoError(t, err)

			// Compare
			assert.Equal(t, cfg.Account.Currency, loaded.Account.Currency)
			assert.Equal(t, cfg.Account.Balance, loaded.Account.Balance)
			assert.Equal(t, cfg.Strategy.RiskPercent, loaded.Strategy.RiskPercent)
			assert.Equal(t, cfg.Strategy.Instrument, loaded.Strategy.Instrument)
		})
	}
}

func TestLoadInvalidFile(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path.yaml")
	assert.Error(t, err)
}

func TestPriceStepParseDuration(t *testing.T) {
	tests := []struct {
		delay    string
		expected string
		wantErr  bool
	}{
		{"1h", "1h0m0s", false},
		{"30m", "30m0s", false},
		{"1s", "1s", false},
		{"", "0s", false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.delay, func(t *testing.T) {
			ps := PriceStep{Delay: tt.delay}
			d, err := ps.ParseDuration()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, d.String())
			}
		})
	}
}
