package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStrategy_EmptyKind(t *testing.T) {
	t.Parallel()

	_, err := GetStrategy(StrategyConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy.kind is required")
}

func TestRegisterStrategy_ErrorsOnBlankName(t *testing.T) {
	ctor := func(map[string]any) (Strategy, error) { return nil, nil }

	err := RegisterStrategy(ctor, "   ")
	require.Error(t, err)
	assert.EqualError(t, err, "RegisterStrategy: blank strategy name")
}

func TestRegisterStrategy_ErrorsOnDuplicateName(t *testing.T) {
	name := "test-duplicate-registry-name"
	ctor := func(map[string]any) (Strategy, error) { return nil, nil }

	require.NoError(t, RegisterStrategy(ctor, name))

	err := RegisterStrategy(ctor, name)
	require.Error(t, err)
	assert.EqualError(t, err, "RegisterStrategy: duplicate strategy name \"test-duplicate-registry-name\"")
}
