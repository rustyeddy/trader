package broker

import (
	"github.com/rustyeddy/trader/internal/account"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
)

// AccountsResponse is the structured result of the Accounts use case.
type AccountsResponse struct {
	Accounts []account.Reference
}

// SnapshotResponse is the structured result of the Snapshot use case.
type SnapshotResponse struct {
	Snapshot account.Snapshot
}

// SubmitResponse is the structured result of the Submit use case: the
// resulting Order in whatever state the broker reports synchronously —
// see brokerpkg.Account.Submit's own doc comment for what that can be.
type SubmitResponse struct {
	Order runtimeorder.Order
}

// CancelResponse is the structured result of the Cancel use case.
type CancelResponse struct {
	Result runtimeorder.CancelResult
}

// ReplaceResponse is the structured result of the Replace use case.
type ReplaceResponse struct {
	Result runtimeorder.ReplaceResult
}
