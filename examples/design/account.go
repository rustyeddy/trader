package main

type money int64

type account struct {
	id      string
	balance money
}

func newAccount(id string, balance money) *account {
	return &account{
		id:      id,
		balance: balance,
	}
}

func (act *account) marginAvailable() money {
	return act.balance
}
