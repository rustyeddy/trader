package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountManager_Add(t *testing.T) {
	t.Parallel()
	am := NewAccountManager()
	act := NewAccount("test", MoneyFromFloat(100_000))
	am.Add(act)
	retrieved := am.Get(act.ID)
	require.NotNil(t, retrieved)
	assert.Equal(t, act.ID, retrieved.ID)
}

func TestAccountManager_Get_Missing(t *testing.T) {
	t.Parallel()
	am := NewAccountManager()
	retrieved := am.Get("nonexistent")
	assert.Nil(t, retrieved)
}

func TestAccountManager_CreateAccount(t *testing.T) {
	t.Parallel()

	am := NewAccountManager()
	act := am.CreateAccount("paper", 100_000)

	require.NotNil(t, act)
	assert.Equal(t, "paper", act.Name)
	assert.Equal(t, MoneyFromFloat(100_000), act.Balance)
	assert.Same(t, act, am.Get("paper"))
}
