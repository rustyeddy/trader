package account

import (
	"fmt"
	"os"
	"strings"

	"github.com/rustyeddy/trader/brokers"
	"github.com/rustyeddy/trader/config/active"
)

// AccountCfg carries the broker/account-id flag state a caller wants
// resolved via ResolveTarget.
type AccountCfg struct {
	Broker           string
	BrokerChanged    bool
	AccountID        string
	AccountIDChanged bool
}

// ResolveTarget resolves the broker and account ID a command should operate
// on.
//
// The account ID is resolved in priority order: an explicit flag value
// (accountIDFlag, when accountIDChanged is true), the OANDA_ACCOUNT_ID env
// var, a caller-supplied config default (configAccountID — e.g. from
// global config), then the CLI's locally persisted "active" selection (see
// the active package). The returned account ID may be "" — a valid state
// meaning nothing resolved a target.
//
// The broker is resolved from an explicit flag value (brokerFlag, when
// brokerChanged is true), then the persisted active selection, defaulting
// to "oanda" — broker has no env/config-file precedent since only one
// exists today.
func ResolveTarget(brokerFlag string, brokerChanged bool, accountIDFlag string, accountIDChanged bool, configAccountID string) (resolvedBroker, resolvedAccountID string, err error) {
	sel, _ := active.Load() // best-effort; missing/unreadable file just means no fallback

	resolvedBroker = "oanda"
	if sel.Broker != "" {
		resolvedBroker = sel.Broker
	}
	if brokerChanged {
		resolvedBroker = brokerFlag
	}
	if !brokers.IsKnownBroker(resolvedBroker) {
		return "", "", fmt.Errorf("unknown broker %q (supported: %s)", resolvedBroker, strings.Join(brokers.KnownBrokers, ", "))
	}

	switch {
	case accountIDChanged:
		resolvedAccountID = accountIDFlag
	case os.Getenv("OANDA_ACCOUNT_ID") != "":
		resolvedAccountID = os.Getenv("OANDA_ACCOUNT_ID")
	case configAccountID != "":
		resolvedAccountID = configAccountID
	case sel.AccountID != "":
		resolvedAccountID = sel.AccountID
	}
	return resolvedBroker, resolvedAccountID, nil
}
