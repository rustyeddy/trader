package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/data"
	tlog "github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/types"
)

type Config struct {
	Symbols         string `json:"symbols"`
	Start           string `json:"start"`
	End             string `json:"end"`
	types.Timeframe `json:"timeframe"`
	Basedir         string `json:"basedir"`
	Dukasdir        string `json:"dukasdir"`
	CandleRoot      string `json:"candleroot"`
	Download        bool
	Candles         bool
}

var (
	config = &Config{
		Symbols:    "EURUSD,USDJPY,GBPUSD",
		Start:      "",
		End:        "",
		Timeframe:  types.D1,
		Dukasdir:   "dukas/",
		CandleRoot: "candles/",
		Download:   false,
		Candles:    false,
	}
)

func init() {
	flag.StringVar(&config.Start, "start", "2025-01-01T00:00:00Z", "start of range")
	flag.StringVar(&config.End, "end", "2025-12-31T00:00:00Z", "end of range")
	flag.StringVar(&config.Symbols, "symbols", "EURUSD,GBPUSD,USDJPY,USDCHF", "Instruments to download")
	flag.BoolVar(&config.Download, "download", false, "Download missing tick files")
	flag.BoolVar(&config.Candles, "build", false, "Download missing tick files")
}

func main() {
	start := time.Now()
	flag.Parse()

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dm := &data.DataManager{
		Start:       time.Date(2004, 01, 01, 0, 0, 0, 0, time.UTC),
		End:         time.Now().AddDate(0, 0, -1), // start from yesterday (the last fullday)
		Instruments: strings.Split(config.Symbols, ","),
	}
	dm.Init()
	if err := dm.Sync(ctx, config.Download, config.Candles); err != nil {
		tlog.Fatal("data sync failed", "err", err)
	}
	elapsed := time.Since(start)
	fmt.Printf("Program duration: %s\n", elapsed)
}
