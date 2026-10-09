package backtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectiveSymbols(t *testing.T) {
	tests := []struct {
		name          string
		flags         []string
		symbol, multi string
		want          []string
		wantErr       bool
	}{
		{"flags win over everything", []string{"AUDUSD"}, "EURUSD", "GBPUSD,USDJPY", []string{"AUDUSD"}, false},
		{"symbols list", nil, "", "EURUSD,GBPUSD", []string{"EURUSD", "GBPUSD"}, false},
		{"whitespace and empty entries are dropped", nil, "", " EURUSD , ,GBPUSD,", []string{"EURUSD", "GBPUSD"}, false},
		{"folded YAML scalar spacing", nil, "", "EURUSD, GBPUSD, USDJPY", []string{"EURUSD", "GBPUSD", "USDJPY"}, false},
		{"single symbol", nil, "EURUSD", "", []string{"EURUSD"}, false},
		{"symbols beats symbol", nil, "EURUSD", "GBPUSD", []string{"GBPUSD"}, false},
		{"nothing at all", nil, "", "", nil, true},
		{"only separators", nil, "", " , ,", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := effectiveSymbols(tt.flags, tt.symbol, tt.multi)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
