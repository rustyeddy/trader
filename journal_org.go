package trader

import (
	"fmt"
	"strings"
)

// FormatTradeOrg renders a TradeRecord as an Org-mode block suitable for pasting into a journal.
// It purposely includes narrative placeholders (Thesis/Execution/Review) while keeping all
// structured facts in a PROPERTIES drawer for easy search.
func FormatTradeOrg(t TradeRecord) string {
	heading := fmt.Sprintf("** Trade: %s (%s)", t.Instrument, ShortDisplayID(t.TradeID))

	var b strings.Builder
	b.WriteString(heading)
	b.WriteString("\n")
	b.WriteString(":PROPERTIES:\n")
	writeOrgProperty(&b, "TRADE_ID", t.TradeID)
	writeOrgProperty(&b, "INSTRUMENT", t.Instrument)
	writeOrgProperty(&b, "UNITS", t.Units.String())
	writeOrgProperty(&b, "ENTRY_PRICE", formatNumber(t.EntryPrice, int32(PriceScale)))
	writeOrgProperty(&b, "EXIT_PRICE", formatNumber(t.ExitPrice, int32(PriceScale)))
	writeOrgProperty(&b, "OPEN_TIME", t.OpenTime.String())
	writeOrgProperty(&b, "CLOSE_TIME", t.CloseTime.String())
	writeOrgProperty(&b, "REALIZED_PL", fmt.Sprintf("%.2f", t.RealizedPL.Float64()))
	writeOrgProperty(&b, "REASON", t.Reason)
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

func writeOrgProperty(b *strings.Builder, key, value string) {
	b.WriteString(":")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}
