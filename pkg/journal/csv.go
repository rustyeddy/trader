// pkg/journal/csv.go
package journal

import (
	"encoding/csv"
	"os"
	"strconv"
	"time"
)

type CSVJournal struct {
	trades *csv.Writer
	equity *csv.Writer
	tf, ef *os.File
}

func NewCSV(tradesPath, equityPath string) (*CSVJournal, error) {
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

	return &CSVJournal{tw, ew, tf, ef}, nil
}

func (j *CSVJournal) RecordTrade(t TradeRecord) error {
	j.trades.Write([]string{
		t.TradeID,
		t.Instrument,
		f(t.Units),
		f(t.EntryPrice),
		f(t.ExitPrice),
		t.OpenTime.Format(time.RFC3339),
		t.CloseTime.Format(time.RFC3339),
		f(t.RealizedPL),
		t.Reason,
	})
	j.trades.Flush()
	return nil
}

func (j *CSVJournal) RecordEquity(e EquitySnapshot) error {
	err := j.equity.Write([]string{
		e.Time.Format(time.RFC3339),
		f(e.Balance),
		f(e.Equity),
		f(e.MarginUsed),
		f(e.FreeMargin),
		f(e.MarginLevel),
	})
	if err != nil {
		return err
	}

	j.equity.Flush()
	return nil
}

func (j *CSVJournal) Close() error {
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
