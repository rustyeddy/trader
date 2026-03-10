package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/types"
)

type Config struct {
	Symbols         string `"json":symbols`
	Start           string `"json":start`
	End             string `"json":end`
	types.Timeframe `"json":timeframe`
	Basedir         string `"json":basedir`
	Dukasdir        string `"json":basedir`
}

var (
	config = &Config{
		Symbols:   "EURUSD,USDJPY,GBPUSD",
		Start:     "",
		End:       "",
		Timeframe: types.D1,
		Basedir:   "../../tmp/",
		Dukasdir:  "dukas/",
	}
)

func init() {
	flag.StringVar(&config.Start, "start", "2003-01-01T00:00:00Z", "start of range")
	flag.StringVar(&config.End, "end", "2003-01-01T00:00:00Z", "end of range")
	flag.StringVar(&config.Symbols, "symbols", "EURUSD,USDJPY,GBPUSD", "Instruments to download")
	flag.StringVar(&config.Basedir, "basedir", "../../tmp/dukas", "Basedirectory")
}

func main() {
	flag.Parse()
	if len(os.Args) < 2 {
		fmt.Println("Please give me a command (validate|build)")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dm := &data.DataManager{
		Start:       time.Date(2003, 01, 01, 0, 0, 0, 0, time.UTC),
		End:         time.Now().AddDate(0, 0, -1), // start from yesterday (the last fullday)
		Basedir:     config.Basedir + config.Dukasdir,
		Instruments: strings.Split(config.Symbols, ","),
	}
	dm.Init()

	switch os.Args[1] {

	case "inventory":
		inventory(ctx)

	// XXX These are all obsolete
	case "validate":
		dm.Validate(ctx)

	case "build":
		// Assume M1
		dm.BuildDatasets(ctx)

	case "candles":
		dm.BuildCandles(ctx)

	default:
		fmt.Printf("I don't know what this means", os.Args[1])
		os.Exit(1)
	}
}

func inventory(context.Context) {
	builder := data.NewInventoryBuilder("../../tmp/dukas", "../../tmp/candles")
	inv, err := builder.Build()
	if err != nil {
		fmt.Println(err)
	}
	missingM1 := inv.MissingYears("dukascopy", "EURUSD", data.TickData, types.M1, 2003, 2026)
	staleH1, err := inv.StaleDerived("dukascopy", "EURUSD", types.H1, 2025)
	fmt.Printf("missing m1: %+v\n", missingM1)
	fmt.Printf("stale   h1: %+v\n", staleH1)
}
