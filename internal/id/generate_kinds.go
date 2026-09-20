package id

// Concrete Generate<Kind> functions for each of the seven identifier
// kinds — thin wrappers over the generic Generate[K], the same pattern
// parse.go uses for Parse<Kind>.

// GenerateRunID generates a new RunID.
func GenerateRunID(g *Generator) (RunID, error) { return Generate[runKind](g) }

// GenerateOrderID generates a new OrderID.
func GenerateOrderID(g *Generator) (OrderID, error) { return Generate[orderKind](g) }

// GenerateFillID generates a new FillID.
func GenerateFillID(g *Generator) (FillID, error) { return Generate[fillKind](g) }

// GenerateEventID generates a new EventID.
func GenerateEventID(g *Generator) (EventID, error) { return Generate[eventKind](g) }

// GenerateCorrelationID generates a new CorrelationID.
func GenerateCorrelationID(g *Generator) (CorrelationID, error) { return Generate[correlationKind](g) }

// GenerateAccountID generates a new AccountID. Per the package doc comment,
// call this only when creating a new account entity; persist the result
// and reuse it, never regenerate it on a later run.
func GenerateAccountID(g *Generator) (AccountID, error) { return Generate[accountKind](g) }

// GenerateIntentID generates a new IntentID.
func GenerateIntentID(g *Generator) (IntentID, error) { return Generate[intentKind](g) }
