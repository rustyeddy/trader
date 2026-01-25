package journal

import (
	"fmt"
	"strings"
	"time"
)

// FormatTradeOrg renders a TradeRecord as an Org-mode block suitable for pasting into a journal.
// It purposely includes narrative placeholders (Thesis/Execution/Review) while keeping all
// structured facts in a PROPERTIES drawer for easy search.
func FormatTradeOrg(t TradeRecord) string {
	heading := fmt.Sprintf("** Trade: %s (%s)", t.Instrument, shortID(t.TradeID))
	// Use RFC3339 for copy/paste friendliness.
	open := t.OpenTime.UTC().Format(time.RFC3339)
	close := t.CloseTime.UTC().Format(time.RFC3339)

	var b strings.Builder
	b.WriteString(heading)
	b.WriteString("\n")
	b.WriteString(":PROPERTIES:\n")
	b.WriteString(fmt.Sprintf(":TRADE_ID: %s\n", t.TradeID))
	b.WriteString(fmt.Sprintf(":ID: %s\n", t.TradeID))
	b.WriteString(fmt.Sprintf(":INSTRUMENT: %s\n", t.Instrument))
	b.WriteString(fmt.Sprintf(":UNITS: %.0f\n", t.Units))
	b.WriteString(fmt.Sprintf(":ENTRY_PRICE: %.5f\n", t.EntryPrice))
	b.WriteString(fmt.Sprintf(":EXIT_PRICE: %.5f\n", t.ExitPrice))
	b.WriteString(fmt.Sprintf(":OPEN_TIME: %s\n", open))
	b.WriteString(fmt.Sprintf(":CLOSE_TIME: %s\n", close))
	b.WriteString(fmt.Sprintf(":REALIZED_PL: %.2f\n", t.RealizedPL))
	b.WriteString(fmt.Sprintf(":REASON: %s\n", t.Reason))
	b.WriteString(":END:\n")
	b.WriteString("\n")
	b.WriteString("*** Thesis\n- \n\n")
	b.WriteString("*** Execution\n- \n\n")
	b.WriteString("*** Review\n- \n")

	return b.String()
}

// FormatTradesOrg renders multiple trades separated by blank lines.
func FormatTradesOrg(trades []TradeRecord) string {
	var b strings.Builder
	for i, t := range trades {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(FormatTradeOrg(t))
	}
	return b.String()
}

func shortID(full string) string {
	if len(full) <= 8 {
		return full
	}
	return full[:8]
}
