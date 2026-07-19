package strategy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The Get*Param family itself (int/int32/float64/bool/string/map param
// extraction) is tested in types/params_test.go, where the readers live.

func TestGetStrategy_Unknown(t *testing.T) {
	t.Parallel()

	_, err := GetStrategy(StrategyConfig{Kind: "no-such-strategy"})
	require.Error(t, err)
}
