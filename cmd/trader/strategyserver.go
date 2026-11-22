package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/strategy"
)

// defaultStrategyListen is where Trader accepts strategy clients unless told
// otherwise. An empty --strategy-listen turns it off.
const defaultStrategyListen = "127.0.0.1:9100"

// listenStrategy opens the strategy listener, which must be on loopback. An empty
// addr is not an error: it means the listener is off, and the listener is nil.
func listenStrategy(addr string) (net.Listener, error) {
	if addr == "" {
		return nil, nil
	}
	local, err := loopback(addr)
	if err != nil {
		return nil, fmt.Errorf("--strategy-listen: %w", err)
	}
	if !local {
		return nil, fmt.Errorf("--strategy-listen %q is not a loopback address: strategies connect from this machine only", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("strategy listen: %w (--strategy-listen \"\" turns the strategy listener off)", err)
	}
	return ln, nil
}

// strategyRuns is the handler of the strategy listener: it runs the backtest a
// client asked for through the application service, with the client as the
// strategy. Transport only: the service makes the run, loads the bars and journals.
func strategyRuns(log *slog.Logger, resultsRoot string) strategy.RunHandler {
	return func(ctx context.Context, req strategy.RunRequest, s *strategy.Session) (strategy.Result, error) {
		log.Info("strategy run requested", "client", s.Remote, "instruments", req.Instruments, "interval", req.Interval, "from", req.From, "to", req.To)
		run, err := service.RunBacktest(ctx, log, service.BacktestRequest{
			Instruments: req.Instruments, Interval: req.Interval, From: req.From, To: req.To,
			Remote: s.Client, RemoteAddr: s.Remote,
			ResultsRoot: resultsRoot,
			OnStart:     s.Accept,
		})
		if run == nil { // no run was made: a refusal, which has no RunID
			log.Warn("strategy run refused", "client", s.Remote, "error", err)
			return strategy.Result{}, err
		}
		res := strategy.Result{RunID: run.RunID.String(), Status: run.Status}
		if err != nil {
			res.Error = err.Error()
			log.Warn("strategy run ended", "run", res.RunID, "status", res.Status, "error", err)
			return res, nil
		}
		summary, err := json.Marshal(run.Summary())
		if err != nil {
			return res, fmt.Errorf("summary: %w", err)
		}
		res.Summary = summary
		log.Info("strategy run ended", "run", res.RunID, "status", res.Status)
		return res, nil
	}
}
