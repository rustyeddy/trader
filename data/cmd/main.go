package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rustyeddy/trader/data"
)

type Config struct {
	Start    time.Time
	End      time.Time
	Symbols  string
	Basedir  string
	Dukasdir string
}

var (
	config = &Config{
		Start:    time.Date(2003, 01, 01, 0, 0, 0, 0, time.UTC),
		End:      time.Now().AddDate(0, 0, -1), // start from yesterday (the last fullday)
		Symbols:  "EURUSD,USDJPY,GBPUSD",
		Basedir:  "../../tmp/",
		Dukasdir: "dukas/",
	}
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Please give me a command (validate|build)")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dm := &data.DataManager{
		Start:       config.Start,
		End:         config.End,
		Basedir:     config.Basedir + config.Dukasdir,
		Instruments: strings.Split(config.Symbols, ","),
	}
	dm.Init()

	switch os.Args[1] {
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
