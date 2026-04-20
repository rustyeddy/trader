// journal/csv.go
package trader

import (
	"encoding/csv"
	"os"
	"strconv"
)

type csvJournal struct {
	trades *csv.Writer
	equity *csv.Writer
	tf, ef *os.File
}

func NewCSV(tradesPath, equityPath string) (*csvJournal, error) {
	tf, err := os.Create(tradesPath)
	if err != nil {
		return nil, err
	}
	ef, err := os.Create(equityPath)
	if err != nil {
		return nil, err
	}

	tw := csv.NewWriter(tf)
	ew := csv.NewWriter(ef)

	if err := tw.Write([]string{"trade_id", "instrument", "units", "entry_price", "exit_price", "open_time", "close_time", "realized_pl", "reason"}); err != nil {
		return nil, err
	}
	if err := ew.Write([]string{"time", "balance", "equity", "margin_used", "free_margin", "margin_level"}); err != nil {
		return nil, err
	}

	tw.Flush()
	if err := tw.Error(); err != nil {
		return nil, err
	}
	ew.Flush()
	if err := ew.Error(); err != nil {
		return nil, err
	}

	return &csvJournal{tw, ew, tf, ef}, nil
}

func (j *csvJournal) RecordTrade(t TradeRecord) error {
	err := j.trades.Write([]string{
		t.TradeID,
		t.Instrument,
		t.Units.String(),
		t.EntryPrice.String(),
		t.ExitPrice.String(),
		t.OpenTime.String(),
		t.CloseTime.String(),
		t.RealizedPL.String(),
		t.Reason,
	})
	if err != nil {
		return err
	}
	j.trades.Flush()
	return j.trades.Error()
}

func (j *csvJournal) RecordEquity(e EquitySnapshot) error {
	err := j.equity.Write([]string{
		e.Timestamp.String(),
		e.Balance.String(),
		e.Equity.String(),
		e.MarginUsed.String(),
		e.FreeMargin.String(),
		e.MarginLevel.String(),
	})
	if err != nil {
		return err
	}

	j.equity.Flush()
	return j.equity.Error()
}

func (j *csvJournal) Close() error {
	j.trades.Flush()
	if err := j.trades.Error(); err != nil {
		return err
	}
	j.equity.Flush()
	if err := j.equity.Error(); err != nil {
		return err
	}

	if err := j.tf.Close(); err != nil {
		return err
	}
	if err := j.ef.Close(); err != nil {
		return err
	}
	return nil
}

func f(x float64) string {
	return strconv.FormatFloat(x, 'f', 6, 64)
}
