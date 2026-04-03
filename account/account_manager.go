package account

import "github.com/rustyeddy/trader/types"

type AccountManager struct {
	accounts map[string]*Account
}

func NewAccountManager() *AccountManager {
	return &AccountManager{
		accounts: make(map[string]*Account),
	}
}

func (am *AccountManager) CreateAccount(name string, balance types.Money) *Account {
	act := NewAccount(name, balance)
	am.accounts[name] = act
	return act
}

func (am *AccountManager) Add(act *Account) {
	am.accounts[act.ID] = act
}

func (am *AccountManager) Get(name string) *Account {
	act, _ := am.accounts[name]
	return act
}
