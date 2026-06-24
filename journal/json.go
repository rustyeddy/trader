package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type jsonJournal struct {
	trades *json.Encoder
	equity *json.Encoder
	tf     *os.File
	ef     *os.File
}

func NewJSON(tradesPath, equityPath string) (*jsonJournal, error) {
	tf, err := os.Create(tradesPath)
	if err != nil {
		return nil, err
	}

	ef, err := os.Create(equityPath)
	if err != nil {
		_ = tf.Close()
		return nil, err
	}

	tenc := json.NewEncoder(tf)
	tenc.SetEscapeHTML(false)
	eenc := json.NewEncoder(ef)
	eenc.SetEscapeHTML(false)

	return &jsonJournal{
		trades: tenc,
		equity: eenc,
		tf:     tf,
		ef:     ef,
	}, nil
}

func (j *jsonJournal) RecordTrade(t TradeRecord) error {
	return j.trades.Encode(t)
}

func (j *jsonJournal) RecordEquity(e EquitySnapshot) error {
	return j.equity.Encode(e)
}

func (j *jsonJournal) Close() error {
	if err := j.tf.Close(); err != nil {
		return err
	}
	if err := j.ef.Close(); err != nil {
		return err
	}
	return nil
}

// ReadTradesJSONL reads all TradeRecords from a JSONL file. Malformed/invalid
// lines are silently skipped (forward-compatible with mixed journal data).
func ReadTradesJSONL(path string) ([]TradeRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []TradeRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r TradeRecord
		if err := json.Unmarshal([]byte(line), &r); err == nil {
			records = append(records, r)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func JournalRecordPaths(base string) (tradesPath, equityPath string) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "./trader-journal"
	}

	for _, suffix := range []string{".jsonl", ".json"} {
		base = strings.TrimSuffix(base, suffix)
	}

	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}

	return base + "-trades.jsonl", base + "-equity.jsonl"
}
