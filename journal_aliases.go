package trader

// Transition shim re-exporting the journal package (extracted from the root
// trader package) so existing callers compile unchanged during the migration.
// Remove entries as callers move to github.com/rustyeddy/trader/journal
// directly. See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/journal"

// Journal types.
type (
	Journal        = journal.Journal
	TradeRecord    = journal.TradeRecord
	EquitySnapshot = journal.EquitySnapshot
	LiveJournal    = journal.LiveJournal
)

// Journal constructors and helpers.
var (
	NewCSV             = journal.NewCSV
	NewJSON            = journal.NewJSON
	NewLiveJournal     = journal.NewLiveJournal
	ReadTradesJSONL    = journal.ReadTradesJSONL
	JournalRecordPaths = journal.JournalRecordPaths
	FormatTradeOrg     = journal.FormatTradeOrg
	FormatTradesOrg    = journal.FormatTradesOrg
)
