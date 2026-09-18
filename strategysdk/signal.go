package strategysdk

// DescribedSignal is a guest's own research-side trace of the
// evidence behind one bar's decision — strategysdk's own counterpart
// to journal.Signal, whose shape it mirrors verbatim (Strategy,
// Values), since that type is already a plain, string-keyed evidence
// bag with no Go-interface or identity content to translate
// (ADR-044, ADR-062's own "guest journaling" section). The host
// writes each one through its own retained env.Journal exactly as it
// turns a DescribedIntent into a canonical order.Intent through
// env.Intents; when the host's run has no Journal configured at all,
// every DescribedSignal a guest sends is silently discarded, mirroring
// how an in-process strategy that checks env.Journal != nil before
// calling Record simply does not record anything when it is nil.
type DescribedSignal struct {
	// Strategy identifies which strategy produced this signal — its
	// Descriptor.Name, conventionally.
	Strategy string
	// Values holds this signal's own evidence, string-keyed and
	// strategy-defined — every value must already be canonical,
	// deterministic text (a decimal price/rate string, an enum name,
	// and so on), matching journal.Signal.Values' own contract.
	Values map[string]string
	// CorrelationToken names the same DescribedIntent.CorrelationToken
	// this signal explains, if any — the host resolves it to that
	// group's own real CorrelationID, reproducing
	// strategy/smatrend's own recordSignal behavior across the
	// process boundary (strategy.proto's own DescribedSignal doc
	// comment). Empty means "no intents this bar," the same zero
	// CorrelationID smatrend itself records in that case.
	CorrelationToken string
}

// Signal builds a DescribedSignal for strategy with the given
// evidence values.
func Signal(strategy string, values map[string]string) DescribedSignal {
	return DescribedSignal{Strategy: strategy, Values: values}
}

// WithCorrelation returns a copy of s carrying token — see
// DescribedIntent.WithCorrelation's own doc comment; s and the
// DescribedIntent(s) it explains should share the identical token.
func (s DescribedSignal) WithCorrelation(token string) DescribedSignal {
	s.CorrelationToken = token
	return s
}
