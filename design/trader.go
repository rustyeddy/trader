package main

import (
	"context"
	"os"
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) intentLoop(ctx context.Context) (intentQ chan intent) {
	intentQ = make(chan intent)
	go func() {

		select {
		case i := <-intentQ:
			tr.handleIntent(ctx, i)

		case <-ctx.Done():
			os.Exit(0)
		}

	}()
	return intentQ
}

func (tr *trader) handleIntent(ctx context.Context, i intent) {
	switch i.kind {
	case "open":
		if tr.account.marginAvailable() <= 0 {
			return
		}
		tr.openTrade(i)

	case "close":
		tr.closeTrade(i)
	}

}

func (tr *trader) openTrade(i intent) error {

	return nil
}

func (tr *trader) closeTrade(i intent) error {

	return nil
}
