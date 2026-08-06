package id

// Kind identifies which of Trader's identifier types a value belongs to. It
// supplies the short prefix used in that type's canonical string form (see
// the package doc comment) — what makes a stray ID string in a log line or
// database column identifiable without cross-referencing code.
//
// Kind is implemented only by the unexported marker types below; it is not
// meant to be implemented by callers.
type Kind interface {
	Prefix() string
}

type (
	runKind         struct{}
	sessionKind     struct{}
	intentKind      struct{}
	proposalKind    struct{}
	orderKind       struct{}
	fillKind        struct{}
	eventKind       struct{}
	correlationKind struct{}
	accountKind     struct{}
	instrumentKind  struct{}
)

func (runKind) Prefix() string         { return "run" }
func (sessionKind) Prefix() string     { return "ses" }
func (intentKind) Prefix() string      { return "int" }
func (proposalKind) Prefix() string    { return "prp" }
func (orderKind) Prefix() string       { return "ord" }
func (fillKind) Prefix() string        { return "fil" }
func (eventKind) Prefix() string       { return "evt" }
func (correlationKind) Prefix() string { return "cor" }
func (accountKind) Prefix() string     { return "acc" }
func (instrumentKind) Prefix() string  { return "ins" }

// Trader's ten identifier kinds. Each is a type alias for a distinct
// instantiation of the generic ID type (see id.go), so RunID and OrderID
// are fully distinct Go types at compile time — a value of one can never be
// assigned where the other is expected — while sharing one generic
// implementation instead of ten hand-copied ones.
type (
	RunID         = ID[runKind]
	SessionID     = ID[sessionKind]
	IntentID      = ID[intentKind]
	ProposalID    = ID[proposalKind]
	OrderID       = ID[orderKind]
	FillID        = ID[fillKind]
	EventID       = ID[eventKind]
	CorrelationID = ID[correlationKind]

	// AccountID identifies one Trader-managed account entity. Unlike the
	// other generated kinds, an AccountID must be generated once when the
	// account entity is first created and then persisted; it must never be
	// regenerated on a later run for the same account. A broker's own
	// account identifier is a separate concept entirely — see the package
	// doc comment.
	AccountID = ID[accountKind]

	// InstrumentID identifies one Trader instrument. Its ULID-based
	// identity here is a placeholder for M1: EUR/USD must not receive a
	// fresh identity every run, so a durable instrument identity is
	// expected to eventually come from a canonical instrument registry
	// (see the architecture document's instrument package) rather than
	// routine per-run generation through this package.
	InstrumentID = ID[instrumentKind]
)
