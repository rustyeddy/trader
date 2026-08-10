package id

// Concrete Parse<Kind> and MustParse<Kind> functions for each of the six
// identifier kinds. These are thin wrappers over the generic Parse[K] and
// MustParse[K], instantiated here with the unexported kind marker types
// (runKind, orderKind, ...) that callers outside this package cannot name
// directly — id.Parse[id.OrderID] does not compile, since OrderID is an
// alias for ID[orderKind], not for orderKind itself, and only orderKind
// implements Kind. ParseOrderID and its siblings are therefore the only way
// to parse a given kind from outside this package.

// ParseRunID parses s as a RunID. See Parse.
func ParseRunID(s string) (RunID, error) { return Parse[runKind](s) }

// MustParseRunID is like ParseRunID but panics on error. See MustParse.
func MustParseRunID(s string) RunID { return MustParse[runKind](s) }

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
