package strategy

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var goodRequest = RunRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1), To: utc(2024, 2)}

// startServer runs ServeRuns on a loopback listener until the test ends, and
// returns the address and a function that stops it and returns what ServeRuns did.
func startServer(t *testing.T, handle RunHandler) (addr string, stop func() error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeRuns(ctx, ln, handle) }()
	var once sync.Once
	var result error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				result = errors.New("ServeRuns did not return")
			}
		})
		return result
	}
	t.Cleanup(func() { _ = stop() })
	return ln.Addr().String(), stop
}

// echoRun is a handler that accepts with a RunID, asks the client about one group
// of bars and ends completed, with the number of orders it was given as the summary.
func echoRun(id string) RunHandler {
	return func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		if err := s.Accept(id); err != nil {
			return Result{}, err
		}
		orders, err := s.Client.OnBars(ctx, []marketdata.Bar{{Instrument: req.Instruments[0], Volume: 1}}, nil)
		if err != nil {
			return Result{RunID: id, Status: StatusFailed}, err
		}
		return Result{RunID: id, Status: StatusCompleted, Summary: []byte(`{"orders":` + itoa(len(orders)) + `}`)}, nil
	}
}

func itoa(n int) string { return string(rune('0' + n)) }

func askForOneOrder(bars []marketdata.Bar, _ []Event) ([]Order, error) {
	return []Order{{Instrument: bars[0].Instrument, Action: "close"}}, nil
}

func TestServeRuns_ServesARunAndSendsTheResult(t *testing.T) {
	addr, _ := startServer(t, echoRun("run_1"))

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	require.NoError(t, err)
	assert.Equal(t, "run_1", res.RunID)
	assert.Equal(t, StatusCompleted, res.Status)
	assert.JSONEq(t, `{"orders":1}`, string(res.Summary))
}

func TestServeRuns_HandsTheHandlerTheValidatedRequestAndTheClientsAddress(t *testing.T) {
	var got RunRequest
	var remote string
	addr, _ := startServer(t, func(_ context.Context, req RunRequest, s *Session) (Result, error) {
		got, remote = req, s.Remote
		return Result{}, errors.New("not today")
	})

	_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	require.Error(t, err)
	assert.Equal(t, goodRequest, got)
	assert.Contains(t, remote, "127.0.0.1:")
}

func TestServeRuns_ServesClientsBackToBackOnOneServer(t *testing.T) {
	var n int
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		n++
		return echoRun("run_"+itoa(n))(ctx, req, s)
	})

	for i := 1; i <= 3; i++ {
		res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
		require.NoError(t, err, "client %d", i)
		assert.Equal(t, "run_"+itoa(i), res.RunID)
	}
}

func TestServeRuns_ARefusedRunHasNoRunIDAndTheServerGoesOn(t *testing.T) {
	calls := 0
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		calls++
		if calls == 1 {
			return Result{}, errors.New(`interval "m1" is not supported`)
		}
		return echoRun("run_ok")(ctx, req, s)
	})

	_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
	assert.ErrorContains(t, err, `the trader refused the run: interval "m1" is not supported`)

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
	require.NoError(t, err)
	assert.Equal(t, "run_ok", res.RunID)
}

func TestServeRuns_ABadRequestIsRefusedWithoutCallingTheHandlerAndTheServerGoesOn(t *testing.T) {
	calls := 0
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		calls++
		return echoRun("run_ok")(ctx, req, s)
	})

	for name, body := range map[string]string{
		"from after to":    `{"run":{"instruments":["EURUSD"],"interval":"h1","from":"2024-02-01T00:00:00Z","to":"2024-01-01T00:00:00Z"}}`,
		"no instruments":   `{"run":{"instruments":[],"interval":"h1","from":"2024-01-01T00:00:00Z","to":"2024-02-01T00:00:00Z"}}`,
		"an unknown field": `{"run":{"instruments":["EURUSD"],"interval":"h1","from":"2024-01-01T00:00:00Z","to":"2024-02-01T00:00:00Z","units":5}}`,
		"not a run":        `{"bars":[],"events":[]}`,
		"not json":         `nope`,
		"a null run":       `{"run":null}`,
		"trailing garbage": `{"run":{}} x`,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := net.Dial("tcp", addr)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			writeRaw(c, body)
			var start startMessage
			require.NoError(t, readMessage(c, &start))
			assert.NotEmpty(t, start.Error)
			assert.Nil(t, start.Accepted)
		})
	}
	assert.Zero(t, calls)

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
	require.NoError(t, err)
	assert.Equal(t, "run_ok", res.RunID)
}

func TestServeRuns_AClientThatSaysNothingDoesNotHoldTheNextOneUpForever(t *testing.T) {
	prev := requestTimeout
	requestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { requestTimeout = prev })
	addr, _ := startServer(t, echoRun("run_ok"))

	silent, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer func() { _ = silent.Close() }()

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
	require.NoError(t, err)
	assert.Equal(t, "run_ok", res.RunID)
}

// A run may pause for longer than the request timeout between acceptance and the
// first bars, and between the last bars and the result: the deadlines set for the
// acceptance and the result must not cut those waits short.
func TestServeRuns_PausesAfterAcceptanceAndBeforeTheResultAreNotCutShort(t *testing.T) {
	prev := requestTimeout
	requestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { requestTimeout = prev })
	pause := 3 * requestTimeout

	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		if err := s.Accept("run_p"); err != nil {
			return Result{}, err
		}
		time.Sleep(pause) // loading bars
		if _, err := s.Client.OnBars(ctx, []marketdata.Bar{{Instrument: req.Instruments[0], Volume: 1}}, nil); err != nil {
			return Result{RunID: "run_p", Status: StatusFailed}, err
		}
		time.Sleep(pause) // finishing the run
		return Result{RunID: "run_p", Status: StatusCompleted}, nil
	})

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, res.Status)
}

func TestServeRuns_AClientThatLeavesMidRunEndsOnlyThatRun(t *testing.T) {
	var runs int
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		runs++
		return echoRun("run_"+itoa(runs))(ctx, req, s)
	})

	// The first client takes the bars and fails: Run hangs up.
	_, err := Run(context.Background(), addr, goodRequest, func([]marketdata.Bar, []Event) ([]Order, error) {
		return nil, errors.New("strategy crashed")
	})
	assert.ErrorContains(t, err, "strategy crashed")

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
	require.NoError(t, err)
	assert.Equal(t, "run_2", res.RunID, "the server took the next client")
	assert.Equal(t, StatusCompleted, res.Status)
}

func TestServeRuns_AFailureAfterAcceptanceIsAResultWithTheRunID(t *testing.T) {
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		if err := s.Accept("run_f"); err != nil {
			return Result{}, err
		}
		return Result{RunID: "run_f", Status: StatusFailed}, errors.New("no bars for EURUSD")
	})

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	require.NoError(t, err, "a run Trader failed is a result, not a transport error")
	assert.Equal(t, "run_f", res.RunID)
	assert.Equal(t, StatusFailed, res.Status)
	assert.Equal(t, "no bars for EURUSD", res.Error)
	assert.Empty(t, res.Summary)
}

func TestSession_AcceptTwiceIsAnError(t *testing.T) {
	var second error
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		require.NoError(t, s.Accept("run_1"))
		second = s.Accept("run_1")
		return Result{RunID: "run_1", Status: StatusCompleted}, nil
	})

	_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	require.NoError(t, err)
	assert.Error(t, second)
}

// While one run is active the next client waits in the backlog and is served when
// it ends; one that will not wait gives up with an error of its own.
func TestServeRuns_ASecondClientWaitsForTheFirstRun(t *testing.T) {
	hold, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	addr, _ := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		first := false
		once.Do(func() { first = true })
		if first {
			close(started)
			<-hold
		}
		return echoRun("run_x")(ctx, req, s)
	})

	firstDone := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
		firstDone <- err
	}()
	<-started

	t.Run("one that is out of patience gives up", func(t *testing.T) {
		_, err := Run(context.Background(), addr, goodRequest, askForOneOrder, WithQueueTimeout(100*time.Millisecond))
		assert.ErrorContains(t, err, "did not accept the run within")
	})
	t.Run("one that waits is served after the first", func(t *testing.T) {
		second := make(chan error, 1)
		go func() {
			_, err := Run(context.Background(), addr, goodRequest, askForOneOrder, WithQueueTimeout(10*time.Second))
			second <- err
		}()
		time.Sleep(50 * time.Millisecond)
		close(hold)
		require.NoError(t, <-firstDone)
		require.NoError(t, <-second)
	})
}

func TestServeRuns_StoppingItCancelsTheRunInProgressAndReturnsNil(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	addr, stop := startServer(t, func(ctx context.Context, req RunRequest, s *Session) (Result, error) {
		require.NoError(t, s.Accept("run_c"))
		close(started)
		<-ctx.Done()
		close(cancelled)
		return Result{RunID: "run_c", Status: StatusCancelled, Error: "context canceled"}, ctx.Err()
	})

	got := make(chan *Result, 1)
	go func() {
		res, _ := Run(context.Background(), addr, goodRequest, askForOneOrder)
		got <- res
	}()
	<-started

	require.NoError(t, stop())

	<-cancelled
	res := <-got
	require.NotNil(t, res)
	assert.Equal(t, StatusCancelled, res.Status)
	_, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	assert.Error(t, err, "it no longer accepts")
}

func TestServeRuns_RefusesAListenerThatIsNotLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	err = ServeRuns(context.Background(), ln, echoRun("x"))

	assert.ErrorContains(t, err, "not loopback")
}
