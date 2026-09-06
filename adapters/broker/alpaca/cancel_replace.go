package alpaca

import (
	"context"
	"errors"
	"fmt"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
)

// Cancel implements broker.Account. Alpaca cancels asynchronously: a
// successful DELETE means Alpaca accepted the cancel request, not that
// the order is already StatusCanceled. Cancel therefore reports
// StatusPendingCancel synchronously on acceptance — an honest
// reflection of what Alpaca's own response actually confirms — but
// records no event of its own: the polling EventReader's next poll
// naturally observes the same wire status transition (Alpaca's order
// itself already moved to "pending_cancel") and reports it as an
// ordinary EventKindOrder event, exactly the same way it discovers any
// other broker-side status change. The eventual StatusCanceled (or, if
// the market got there first, StatusFilled) transition is discovered
// and reported by that same polling EventReader, the
// same way any other broker-side state change is.
//
// If Alpaca declines the cancel outright (its real 422 response for an
// order that cannot currently be canceled, for example already
// filled/canceled), Cancel re-fetches the order's actual current state
// — DELETE's own response carries no body to report it from — and
// returns that as CancelResult.Status with a Rejection explaining the
// decline, per broker.Account.Cancel's own contract. A 404 means the
// order id itself is unrecognized, matching ErrOrderNotFound.
func (h *accountHandle) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	if err := ctx.Err(); err != nil {
		return order.CancelResult{}, err
	}
	if h.broker.isClosed() {
		return order.CancelResult{}, brokerpkg.ErrClosed
	}
	req, err := order.NewCancelRequest(req)
	if err != nil {
		return order.CancelResult{}, err
	}
	if req.Metadata.EventID.IsZero() {
		return order.CancelResult{}, fmt.Errorf("%w: metadata event id must be set", order.ErrInvalidCancelRequest)
	}

	alpacaOrderID, err := h.resolveAlpacaOrderID(ctx, req.OrderID)
	if err != nil {
		return order.CancelResult{}, err
	}

	cancelErr := h.broker.client.CancelOrder(ctx, alpacaOrderID)
	if cancelErr == nil {
		result, err := order.NewCancelResult(order.CancelResult{
			OrderID: req.OrderID,
			Status:  order.StatusPendingCancel,
			Metadata: id.Metadata{
				CausationID: req.Metadata.EventID,
				Timestamp:   h.broker.deps.Clock.Now(),
			},
		})
		if err != nil {
			return order.CancelResult{}, err
		}
		return result, nil
	}
	if errors.Is(cancelErr, ErrOrderNotFound) {
		return order.CancelResult{}, brokerpkg.ErrOrderNotFound
	}

	// Anything else (Alpaca's 422 "cannot cancel" case, transport
	// failure) — re-fetch the order to report its real current status,
	// since DELETE's own response never carries a body to report it
	// from. If even the re-fetch fails, propagate cancelErr: it is the
	// original, more specific failure.
	wo, getErr := h.broker.client.GetOrder(ctx, alpacaOrderID)
	if getErr != nil {
		return order.CancelResult{}, fmt.Errorf("alpaca: cancel: %w", cancelErr)
	}
	o, translateErr := wireOrderToOrder(wo, h.broker.deps.Resolver, h.broker.name, h.broker.ref.AccountID, h.broker.deps.IDs)
	if translateErr != nil {
		return order.CancelResult{}, fmt.Errorf("alpaca: cancel: %w", cancelErr)
	}
	return order.NewCancelResult(order.CancelResult{
		OrderID: req.OrderID,
		Status:  o.Status,
		Rejection: &order.Rejection{
			Reason:     order.ReasonUnknown,
			Detail:     fmt.Sprintf("alpaca declined the cancel: %v", cancelErr),
			BrokerCode: cancelErr.Error(),
		},
		Metadata: id.Metadata{
			CausationID: req.Metadata.EventID,
			Timestamp:   h.broker.deps.Clock.Now(),
		},
	})
}

// Replace implements broker.Account. Alpaca's PATCH response is the
// resulting order, whether or not Alpaca preserves the original order
// id on replace (unverified in this implementation — see wireshape.go);
// Replace records whatever id the response reports as this order's
// current Alpaca identity going forward, so a subsequent Cancel/Replace
// resolves correctly either way.
//
// A 422 decline is reported the same way Cancel's is: ReplaceResult
// carries the order's actual current status (re-fetched, since PATCH's
// own error response carries no usable order body) and a Rejection
// explaining the decline.
func (h *accountHandle) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	if err := ctx.Err(); err != nil {
		return order.ReplaceResult{}, err
	}
	if h.broker.isClosed() {
		return order.ReplaceResult{}, brokerpkg.ErrClosed
	}
	req, err := order.NewReplaceRequest(req)
	if err != nil {
		return order.ReplaceResult{}, err
	}
	if req.Metadata.EventID.IsZero() {
		return order.ReplaceResult{}, fmt.Errorf("%w: metadata event id must be set", order.ErrInvalidReplaceRequest)
	}

	alpacaOrderID, err := h.resolveAlpacaOrderID(ctx, req.OrderID)
	if err != nil {
		return order.ReplaceResult{}, err
	}

	wireReq := wireOrderReplace{}
	if req.NewQuantity != nil {
		s := req.NewQuantity.String()
		wireReq.Qty = &s
	}
	if req.NewLimitPrice != nil {
		s := req.NewLimitPrice.String()
		wireReq.LimitPrice = &s
	}
	if req.NewStopPrice != nil {
		s := req.NewStopPrice.String()
		wireReq.StopPrice = &s
	}

	wireResp, replaceErr := h.broker.client.ReplaceOrder(ctx, alpacaOrderID, wireReq)
	if replaceErr == nil {
		// Record whatever id Alpaca now reports for this order, whether
		// it kept the original or minted a new one (see this method's
		// own doc comment).
		h.broker.corr.rememberOrderID(req.OrderID, wireResp.ID)
		return order.NewReplaceResult(order.ReplaceResult{
			OrderID: req.OrderID,
			Status:  statusFromWire(wireResp.Status),
			Metadata: id.Metadata{
				CausationID: req.Metadata.EventID,
				Timestamp:   h.broker.deps.Clock.Now(),
			},
		})
	}
	if errors.Is(replaceErr, ErrOrderNotFound) {
		return order.ReplaceResult{}, brokerpkg.ErrOrderNotFound
	}

	wo, getErr := h.broker.client.GetOrder(ctx, alpacaOrderID)
	if getErr != nil {
		return order.ReplaceResult{}, fmt.Errorf("alpaca: replace: %w", replaceErr)
	}
	o, translateErr := wireOrderToOrder(wo, h.broker.deps.Resolver, h.broker.name, h.broker.ref.AccountID, h.broker.deps.IDs)
	if translateErr != nil {
		return order.ReplaceResult{}, fmt.Errorf("alpaca: replace: %w", replaceErr)
	}
	return order.NewReplaceResult(order.ReplaceResult{
		OrderID: req.OrderID,
		Status:  o.Status,
		Rejection: &order.Rejection{
			Reason:     order.ReasonUnknown,
			Detail:     fmt.Sprintf("alpaca declined the replace: %v", replaceErr),
			BrokerCode: replaceErr.Error(),
		},
		Metadata: id.Metadata{
			CausationID: req.Metadata.EventID,
			Timestamp:   h.broker.deps.Clock.Now(),
		},
	})
}
