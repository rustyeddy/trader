package replay

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/sim"
)

// Options controls how replay behaves.
type Options struct {
	// If true: process tick first (UpdatePrice), then event.
	// This is what you want most of the time, so OPEN uses current tick prices,
	// and CLOSE_ALL closes at that tick's prices.
	TickThenEvent bool
}

// CSV replays ticks from a CSV file and applies optional scripted events.
//
// CSV formats supported:
//
//  1. Basic ticks:
//     time,instrument,bid,ask
//
//  2. Ticks + events:
//     time,instrument,bid,ask,event,arg1,arg2,arg3,arg4
//
// Events (case-insensitive):
//
//	OPEN:        arg1=instrument  arg2=units
//	OPEN_SLTP:   arg1=instrument  arg2=units  arg3=stopLoss  arg4=takeProfit
//	CLOSE_ALL:   arg1=reason (optional)
//	CLOSE:       arg1=tradeID     arg2=reason (optional)   (requires Engine.CloseTrade)
//
// Notes:
// - This function calls engine.UpdatePrice() for each row.
// - With TickThenEvent=true, UpdatePrice happens before the event.
// - If you add additional event types later, extend handleEvent().
func CSV(ctx context.Context, csvPath string, engine *sim.Engine, opts Options) error {
	f, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	// Read first row and detect header or data.
	firstRow, err := r.Read()
	if err != nil {
		return err
	}

	hasHeader := len(firstRow) > 0 && strings.EqualFold(strings.TrimSpace(firstRow[0]), "time")
	if !hasHeader {
		if err := handleReplayRow(ctx, engine, firstRow, opts); err != nil {
			return err
		}
	}

	for {
		row, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(row) == 0 {
			continue
		}
		if err := handleReplayRow(ctx, engine, row, opts); err != nil {
			return err
		}
	}
}

func handleReplayRow(ctx context.Context, engine *sim.Engine, row []string, opts Options) error {
	// Minimum tick columns: time,instrument,bid,ask
	if len(row) < 4 {
		return fmt.Errorf("bad row (need at least 4 cols time,instrument,bid,ask): %v", row)
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(row[0]))
	if err != nil {
		return fmt.Errorf("bad time %q: %w", row[0], err)
	}
	inst := strings.TrimSpace(row[1])

	bid, err := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
	if err != nil {
		return fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
	if err != nil {
		return fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	price := broker.Price{
		Time:       t,
		Instrument: inst,
		Bid:        bid,
		Ask:        ask,
	}

	// Optional event columns: event,arg1,arg2,arg3,arg4...
	event := ""
	args := []string{}
	if len(row) >= 5 {
		event = strings.TrimSpace(row[4])
	}
	if len(row) >= 6 {
		args = row[5:]
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
	}

	if opts.TickThenEvent {
		if err := engine.UpdatePrice(price); err != nil {
			return err
		}
		if event != "" {
			return handleEvent(ctx, engine, event, args)
		}
		return nil
	}

	// Event first, then tick (rare, but supported)
	if event != "" {
		if err := handleEvent(ctx, engine, event, args); err != nil {
			return err
		}
	}
	return engine.UpdatePrice(price)
}

func handleEvent(ctx context.Context, engine *sim.Engine, event string, args []string) error {
	switch strings.ToUpper(event) {
	case "OPEN":
		// OPEN,EUR_USD,10000
		inst, units, err := parseOpenArgs(args)
		if err != nil {
			return fmt.Errorf("OPEN: %w", err)
		}
		_, err = engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
			Instrument: inst,
			Units:      units,
		})
		return err

	case "OPEN_SLTP":
		// OPEN_SLTP,EUR_USD,10000,1.0980,1.1050
		inst, units, sl, tp, err := parseOpenSLTPArgs(args)
		if err != nil {
			return fmt.Errorf("OPEN_SLTP: %w", err)
		}

		req := broker.MarketOrderRequest{
			Instrument: inst,
			Units:      units,
			StopLoss:   &sl,
			TakeProfit: &tp,
		}

		_, err = engine.CreateMarketOrder(ctx, req)
		return err

	case "CLOSE_ALL":
		// CLOSE_ALL,Reason
		reason := "ManualClose"
		if len(args) >= 1 && args[0] != "" {
			reason = args[0]
		}
		return engine.CloseAll(ctx, reason)

	case "CLOSE":
		// CLOSE,<tradeID>,<reason>
		// Requires Engine.CloseTrade to exist.
		if len(args) < 1 || args[0] == "" {
			return fmt.Errorf("missing tradeID")
		}
		tradeID := args[0]
		reason := "ManualClose"
		if len(args) >= 2 && args[1] != "" {
			reason = args[1]
		}
		return engine.CloseTrade(ctx, tradeID, reason)

	default:
		return fmt.Errorf("unknown event %q", event)
	}
}

func parseOpenArgs(args []string) (inst string, units float64, err error) {
	if len(args) < 2 {
		return "", 0, fmt.Errorf("need arg1=instrument arg2=units")
	}
	inst = args[0]
	if inst == "" {
		return "", 0, fmt.Errorf("instrument is empty")
	}
	units, err = strconv.ParseFloat(args[1], 64)
	if err != nil {
		return "", 0, fmt.Errorf("bad units %q: %w", args[1], err)
	}
	if units == 0 {
		return "", 0, fmt.Errorf("units must be non-zero")
	}
	return inst, units, nil
}

func parseOpenSLTPArgs(args []string) (inst string, units float64, sl float64, tp float64, err error) {
	if len(args) < 4 {
		return "", 0, 0, 0, fmt.Errorf("need arg1=instrument arg2=units arg3=stopLoss arg4=takeProfit")
	}
	inst = args[0]
	if inst == "" {
		return "", 0, 0, 0, fmt.Errorf("instrument is empty")
	}
	units, err = strconv.ParseFloat(args[1], 64)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("bad units %q: %w", args[1], err)
	}
	if units == 0 {
		return "", 0, 0, 0, fmt.Errorf("units must be non-zero")
	}
	sl, err = strconv.ParseFloat(args[2], 64)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("bad stopLoss %q: %w", args[2], err)
	}
	tp, err = strconv.ParseFloat(args[3], 64)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("bad takeProfit %q: %w", args[3], err)
	}
	return inst, units, sl, tp, nil
}
