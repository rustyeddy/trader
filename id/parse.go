package id

// Concrete Parse<Kind> and MustParse<Kind> functions for each of the ten
// identifier kinds. These are thin wrappers over the generic Parse[K] and
// MustParse[K] — id.Parse[id.OrderID] works too — provided so that each
// kind has its own discoverable, directly named constructor rather than
// requiring every call site to spell out a type parameter.

// ParseRunID parses s as a RunID. See Parse.
func ParseRunID(s string) (RunID, error) { return Parse[runKind](s) }

// MustParseRunID is like ParseRunID but panics on error. See MustParse.
func MustParseRunID(s string) RunID { return MustParse[runKind](s) }

// ParseSessionID parses s as a SessionID. See Parse.
func ParseSessionID(s string) (SessionID, error) { return Parse[sessionKind](s) }

// MustParseSessionID is like ParseSessionID but panics on error. See MustParse.
func MustParseSessionID(s string) SessionID { return MustParse[sessionKind](s) }

// ParseIntentID parses s as an IntentID. See Parse.
func ParseIntentID(s string) (IntentID, error) { return Parse[intentKind](s) }

// MustParseIntentID is like ParseIntentID but panics on error. See MustParse.
func MustParseIntentID(s string) IntentID { return MustParse[intentKind](s) }

// ParseProposalID parses s as a ProposalID. See Parse.
func ParseProposalID(s string) (ProposalID, error) { return Parse[proposalKind](s) }

// MustParseProposalID is like ParseProposalID but panics on error. See MustParse.
func MustParseProposalID(s string) ProposalID { return MustParse[proposalKind](s) }

// ParseOrderID parses s as an OrderID. See Parse.
func ParseOrderID(s string) (OrderID, error) { return Parse[orderKind](s) }

// MustParseOrderID is like ParseOrderID but panics on error. See MustParse.
func MustParseOrderID(s string) OrderID { return MustParse[orderKind](s) }

// ParseFillID parses s as a FillID. See Parse.
func ParseFillID(s string) (FillID, error) { return Parse[fillKind](s) }

// MustParseFillID is like ParseFillID but panics on error. See MustParse.
func MustParseFillID(s string) FillID { return MustParse[fillKind](s) }

// ParseEventID parses s as an EventID. See Parse.
func ParseEventID(s string) (EventID, error) { return Parse[eventKind](s) }

// MustParseEventID is like ParseEventID but panics on error. See MustParse.
func MustParseEventID(s string) EventID { return MustParse[eventKind](s) }

// ParseCorrelationID parses s as a CorrelationID. See Parse.
func ParseCorrelationID(s string) (CorrelationID, error) { return Parse[correlationKind](s) }

// MustParseCorrelationID is like ParseCorrelationID but panics on error. See MustParse.
func MustParseCorrelationID(s string) CorrelationID { return MustParse[correlationKind](s) }

// ParseAccountID parses s as an AccountID. See Parse.
func ParseAccountID(s string) (AccountID, error) { return Parse[accountKind](s) }

// MustParseAccountID is like ParseAccountID but panics on error. See MustParse.
func MustParseAccountID(s string) AccountID { return MustParse[accountKind](s) }

// ParseInstrumentID parses s as an InstrumentID. See Parse.
func ParseInstrumentID(s string) (InstrumentID, error) { return Parse[instrumentKind](s) }

// MustParseInstrumentID is like ParseInstrumentID but panics on error. See MustParse.
func MustParseInstrumentID(s string) InstrumentID { return MustParse[instrumentKind](s) }
