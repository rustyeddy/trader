package journal

import (
	"path/filepath"
	"testing"
)

func TestOpen_CSV(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(Config{
		Kind:       "csv",
		TradesPath: filepath.Join(dir, "trades.csv"),
		EquityPath: filepath.Join(dir, "equity.csv"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer j.Close()
}

func TestOpen_JSON(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(Config{
		Kind:       "json",
		TradesPath: filepath.Join(dir, "trades.jsonl"),
		EquityPath: filepath.Join(dir, "equity.jsonl"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer j.Close()
}

func TestOpen_UnknownKindErrors(t *testing.T) {
	_, err := Open(Config{Kind: "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}
