package trader

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dummyLive is a minimal LiveStrategy for registry tests.
type dummyLive struct{ name string }

func (d *dummyLive) Name() string { return d.name }
func (d *dummyLive) Tick(_ context.Context, _ LivePrice, _ []LiveTrade) *LivePlan {
	return &LivePlan{Reason: "dummy"}
}

func dummyCtor(name string) LiveStrategyConstructor {
	return func(_ map[string]any) (LiveStrategy, error) {
		return &dummyLive{name: name}, nil
	}
}

// ── RegisterLiveStrategy ──────────────────────────────────────────────────────

func TestRegisterLiveStrategy_NilCtorReturnsError(t *testing.T) {
	err := RegisterLiveStrategy(nil, "live-reg-nil")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil constructor")
}

func TestRegisterLiveStrategy_NoNamesReturnsError(t *testing.T) {
	err := RegisterLiveStrategy(dummyCtor("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no names provided")
}

func TestRegisterLiveStrategy_BlankNameReturnsError(t *testing.T) {
	err := RegisterLiveStrategy(dummyCtor("x"), "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank name")
}

func TestRegisterLiveStrategy_DuplicateNameReturnsError(t *testing.T) {
	name := "live-reg-dup-" + NewULID()
	require.NoError(t, RegisterLiveStrategy(dummyCtor("first"), name))
	err := RegisterLiveStrategy(dummyCtor("second"), name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestRegisterLiveStrategy_Success(t *testing.T) {
	name := "live-reg-ok-" + NewULID()
	require.NoError(t, RegisterLiveStrategy(dummyCtor(name), name))
	ctor := LookupLiveStrategy(name)
	require.NotNil(t, ctor)
}

func TestRegisterLiveStrategy_NormalizesName(t *testing.T) {
	// Registration with mixed case / spaces should be retrievable via lowercase.
	name := "live-reg-norm-" + NewULID()
	require.NoError(t, RegisterLiveStrategy(dummyCtor(name), "  "+name+"  "))
	assert.NotNil(t, LookupLiveStrategy(name))
}

// ── LookupLiveStrategy ────────────────────────────────────────────────────────

func TestLookupLiveStrategy_UnknownReturnsNil(t *testing.T) {
	assert.Nil(t, LookupLiveStrategy("live-lookup-nonexistent"))
}

func TestLookupLiveStrategy_CaseInsensitive(t *testing.T) {
	name := "live-lookup-case-" + NewULID()
	require.NoError(t, RegisterLiveStrategy(dummyCtor(name), name))
	assert.NotNil(t, LookupLiveStrategy(name))
}

// ── GetLiveStrategy ───────────────────────────────────────────────────────────

func TestGetLiveStrategy_EmptyKindReturnsError(t *testing.T) {
	_, err := GetLiveStrategy(strategy.StrategyConfig{Kind: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestGetLiveStrategy_UnknownKindReturnsError(t *testing.T) {
	_, err := GetLiveStrategy(strategy.StrategyConfig{Kind: "live-get-nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported live strategy")
}

func TestGetLiveStrategy_Success(t *testing.T) {
	name := "live-get-ok-" + NewULID()
	require.NoError(t, RegisterLiveStrategy(dummyCtor(name), name))

	strat, err := GetLiveStrategy(strategy.StrategyConfig{Kind: name})
	require.NoError(t, err)
	require.NotNil(t, strat)
	assert.Equal(t, name, strat.Name())
}

func TestGetLiveStrategy_CtorErrorPropagated(t *testing.T) {
	name := "live-get-err-" + NewULID()
	failCtor := func(_ map[string]any) (LiveStrategy, error) {
		return nil, fmt.Errorf("ctor failed")
	}
	require.NoError(t, RegisterLiveStrategy(failCtor, name))
	_, err := GetLiveStrategy(strategy.StrategyConfig{Kind: name})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ctor failed")
}

// ── RegisteredLiveStrategies ──────────────────────────────────────────────────

func TestRegisteredLiveStrategies_IncludesRegistered(t *testing.T) {
	name := "live-list-" + NewULID()
	require.NoError(t, RegisterLiveStrategy(dummyCtor(name), name))
	names := RegisteredLiveStrategies()
	// Registry normalizes names to lowercase.
	assert.Contains(t, names, strings.ToLower(name))
}

func TestRegisteredLiveStrategies_IsSorted(t *testing.T) {
	names := RegisteredLiveStrategies()
	for i := 1; i < len(names); i++ {
		assert.LessOrEqual(t, names[i-1], names[i], "RegisteredLiveStrategies should be sorted")
	}
}

// ── MustRegisterLiveStrategy ─────────────────────────────────────────────────

func TestMustRegisterLiveStrategy_PanicsOnDuplicate(t *testing.T) {
	name := "live-must-dup-" + NewULID()
	MustRegisterLiveStrategy(dummyCtor(name), name)
	assert.Panics(t, func() {
		MustRegisterLiveStrategy(dummyCtor(name), name)
	})
}
