package alpaca

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// accountHandle is broker.Account bound to a Broker's single configured
// account. Obtain one from Broker.OpenAccount.
type accountHandle struct {
	broker *Broker
}

var _ brokerpkg.Account = (*accountHandle)(nil)

// Reference implements broker.Account.
func (h *accountHandle) Reference() account.Reference {
	return h.broker.ref
}

// Snapshot implements broker.Account. It calls GetAccount and
// ListPositions and ListOrders(status=open) against Alpaca directly —
// unlike adapters/broker/sim, this adapter holds no local ledger of its
// own; Alpaca is authoritative and every Snapshot reflects a fresh read.
func (h *accountHandle) Snapshot(ctx context.Context) (account.Snapshot, error) {
	if h.broker.isClosed() {
		return account.Snapshot{}, brokerpkg.ErrClosed
	}

	wireAcct, err := h.broker.client.GetAccount(ctx)
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: get account: %w", err)
	}
	wirePositions, err := h.broker.client.ListPositions(ctx)
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: list positions: %w", err)
	}
	wireOpenOrders, err := h.broker.client.ListOrders(ctx, ListOrdersOptions{Status: "open"})
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: list orders: %w", err)
	}

	currency, err := num.ParseCurrency(wireAcct.Currency)
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: parse account currency %q: %w", wireAcct.Currency, err)
	}
	unrealized, err := num.ParseMoney("0", currency)
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: zero money: %w", err)
	}

	positions := make([]order.Position, 0, len(wirePositions))
	for _, wp := range wirePositions {
		p, err := wirePositionToPosition(wp, h.broker.deps.Resolver, h.broker.name, h.broker.ref.AccountID)
		if err != nil {
			return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: %w", err)
		}
		positions = append(positions, p)

		pl, err := num.ParseMoney(orDefault(wp.UnrealizedPL, "0"), currency)
		if err != nil {
			return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: parse unrealized_pl %q: %w", wp.UnrealizedPL, err)
		}
		unrealized, err = unrealized.Add(pl)
		if err != nil {
			return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: sum unrealized pnl: %w", err)
		}
	}

	openOrders := make([]order.Order, 0, len(wireOpenOrders))
	for _, wo := range wireOpenOrders {
		o, err := wireOrderToOrder(wo, h.broker.deps.Resolver, h.broker.name, h.broker.ref.AccountID, h.broker.deps.IDs)
		if err != nil {
			return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: %w", err)
		}
		h.broker.corr.rememberOrderID(o.Request.OrderID, o.BrokerOrderID)
		openOrders = append(openOrders, o)
	}

	params, err := wireAccountToSnapshotParams(wireAcct, positions, openOrders, unrealized, h.broker.ref.AccountID, h.broker.name, h.broker.deps.Clock.Now())
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("alpaca: snapshot: %w", err)
	}
	return account.NewSnapshot(params)
}

// Submit implements broker.Account. It translates req into an Alpaca
// order submission, using req.OrderID as Alpaca's client_order_id (see
// the package doc comment for why that alone gives this adapter
// submission idempotency with no local dedup table). Only
// req.Type == order.Market is supported; any other type reports
// broker.ErrUnsupported without ever calling Alpaca.
//
// A 422 (order-level rejection, see wireshape.go's classifyStatus) is
// translated into a StatusRejected order.Order returned with a nil
// error — matching Submit's own doc comment ("returns the resulting
// Order in whatever state the broker reports synchronously"), which
// includes an outright decline. Every other failure to reach or parse
// Alpaca's response propagates as a plain Go error.
func (h *accountHandle) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	if h.broker.isClosed() {
		return order.Order{}, brokerpkg.ErrClosed
	}

	wireReq, err := requestToWireSubmit(req)
	if err != nil {
		return order.Order{}, err
	}

	wireResp, err := h.broker.client.SubmitOrder(ctx, wireReq)
	if err != nil {
		if errors.Is(err, ErrOrderRejected) {
			rejected, buildErr := order.NewOrder(order.Order{
				Request: req,
				Status:  order.StatusRejected,
				Rejection: &order.Rejection{
					Reason:     order.ReasonUnknown,
					Detail:     "alpaca declined the order",
					BrokerCode: err.Error(),
				},
			})
			if buildErr != nil {
				return order.Order{}, fmt.Errorf("alpaca: submit: build rejected order: %w", buildErr)
			}
			return rejected, nil
		}
		return order.Order{}, fmt.Errorf("alpaca: submit: %w", err)
	}

	o, err := wireOrderToOrder(wireResp, h.broker.deps.Resolver, h.broker.name, h.broker.ref.AccountID, h.broker.deps.IDs)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: submit: %w", err)
	}

	h.broker.corr.rememberOrderID(req.OrderID, wireResp.ID)
	// prevState is always the zero observedOrderState here (this
	// Alpaca order id has never been observed before) — captured from
	// observe's own return rather than assumed, so this stays correct
	// even if that ever changes. If Alpaca's POST /v2/orders response
	// already reports the order filled (a small paper market order
	// commonly fills synchronously, within the HTTP response window),
	// emitFillIfIncreased synthesizes the corresponding EventKindFill
	// immediately — see its own doc comment for the bug this fixes
	// (PR #314 review): without it, this call alone would have
	// recorded the already-final filled quantity as the correlator's
	// baseline, so the next poll would see no increase and never
	// produce a Fill event at all for an order that Alpaca filled
	// synchronously.
	newState := observedOrderState{status: wireResp.Status, filledQty: orDefault(wireResp.FilledQty, "0")}
	prevState := h.broker.corr.observe(wireResp.ID, newState)
	if _, err := h.broker.corr.appendEvent(func(sequence uint64) (brokerpkg.Event, error) {
		return buildOrderEvent(h.broker.deps, o, req.Metadata.EventID, sequence)
	}); err != nil {
		return order.Order{}, fmt.Errorf("alpaca: submit: record event: %w", err)
	}
	if err := emitFillIfIncreased(h.broker, prevState, newState, o); err != nil {
		return order.Order{}, fmt.Errorf("alpaca: submit: %w", err)
	}
	return o, nil
}

// resolveAlpacaOrderID resolves req's Trader OrderID to Alpaca's native
// order id: first from this adapter's own in-memory correlator (the
// common case, populated by Submit or a prior Snapshot/poll), falling
// back to Alpaca's real, documented lookup-by-client-order-id endpoint
// for an accountHandle that never itself observed the submission (for
// example, a freshly started process) — matching broker state being
// architecturally authoritative rather than this adapter's own memory.
func (h *accountHandle) resolveAlpacaOrderID(ctx context.Context, orderID id.OrderID) (string, error) {
	if alpacaID, ok := h.broker.corr.lookupOrderID(orderID); ok {
		return alpacaID, nil
	}
	wo, err := h.broker.client.GetOrderByClientOrderID(ctx, orderID.String())
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return "", brokerpkg.ErrOrderNotFound
		}
		return "", fmt.Errorf("alpaca: resolve order id: %w", err)
	}
	h.broker.corr.rememberOrderID(orderID, wo.ID)
	return wo.ID, nil
}

// buildOrderEvent constructs the EventKindOrder Event recording o, at
// sequence, timestamped/identified from deps. Mirrors
// adapters/broker/sim's identical helper.
func buildOrderEvent(deps Deps, o order.Order, causationID id.EventID, sequence uint64) (brokerpkg.Event, error) {
	eventID, err := id.GenerateEventID(deps.IDs)
	if err != nil {
		return brokerpkg.Event{}, err
	}
	now := deps.Clock.Now()
	return brokerpkg.NewEvent(brokerpkg.Event{
		Metadata: id.Metadata{
			EventID:     eventID,
			CausationID: causationID,
			Timestamp:   now,
		},
		ObservedAt: now,
		Sequence:   sequence,
		Kind:       brokerpkg.EventKindOrder,
		Order:      &o,
	})
}
