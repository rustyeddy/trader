package journal

import (
	"encoding/csv"
	"errors"
	"os"
)

var tradeCSVHeader = []string{
	"trade_id", "instrument", "units", "entry_price", "exit_price", "open_time", "close_time", "realized_pl", "reason",
}

var equityCSVHeader = []string{
	"time", "balance", "equity", "margin_used", "free_margin", "margin_level",
}

type csvJournal struct {
	tradeWriter  *csv.Writer
	equityWriter *csv.Writer
	tradesFile   *os.File
	equityFile   *os.File
}

func NewCSV(tradesPath, equityPath string) (*csvJournal, error) {
	tradesFile, tradeWriter, err := openCSVJournalFile(tradesPath, tradeCSVHeader)
	if err != nil {
		return nil, err
	}

	equityFile, equityWriter, err := openCSVJournalFile(equityPath, equityCSVHeader)
	if err != nil {
		_ = tradesFile.Close()
		return nil, err
	}

	return &csvJournal{
		tradeWriter:  tradeWriter,
		equityWriter: equityWriter,
		tradesFile:   tradesFile,
		equityFile:   equityFile,
	}, nil
}

func openCSVJournalFile(path string, header []string) (*os.File, *csv.Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	writer := csv.NewWriter(file)
	if info.Size() == 0 {
		if err := writer.Write(header); err != nil {
			_ = file.Close()
			return nil, nil, err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			_ = file.Close()
			return nil, nil, err
		}
	}

	return file, writer, nil
}

func (j *csvJournal) RecordTrade(t TradeRecord) error {
	err := j.tradeWriter.Write([]string{
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
	j.tradeWriter.Flush()
	return j.tradeWriter.Error()
}

func (j *csvJournal) RecordEquity(e EquitySnapshot) error {
	err := j.equityWriter.Write([]string{
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

	j.equityWriter.Flush()
	return j.equityWriter.Error()
}

func (j *csvJournal) Close() error {
	var errs []error

	j.tradeWriter.Flush()
	errs = append(errs, j.tradeWriter.Error())

	j.equityWriter.Flush()
	errs = append(errs, j.equityWriter.Error())

	errs = append(errs, j.tradesFile.Close(), j.equityFile.Close())

	return errors.Join(errs...)
}
