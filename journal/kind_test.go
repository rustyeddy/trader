package journal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rustyeddy/trader/journal"
)

func TestKindStringNamesEveryDefinedValue(t *testing.T) {
	cases := map[journal.Kind]string{
		journal.KindRunStarted:   "run-started",
		journal.KindIntent:       "intent",
		journal.KindProposal:     "proposal",
		journal.KindDecision:     "decision",
		journal.KindRequest:      "request",
		journal.KindOrder:        "order",
		journal.KindFill:         "fill",
		journal.KindAccount:      "account",
		journal.KindStatus:       "status",
		journal.KindTrade:        "trade",
		journal.KindRunCompleted: "run-completed",
	}
	for k, want := range cases {
		assert.Equal(t, want, k.String())
	}
}

func TestKindStringUnrecognizedValue(t *testing.T) {
	assert.Equal(t, "Kind(200)", journal.Kind(200).String())
}
