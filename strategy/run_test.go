package strategy

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTrader is a hand-written Trader at the address: it reads the run request
// into got, then does what script says on the connection.
func fakeTrader(t *testing.T, script func(c net.Conn)) (addr string, got <-chan runMessage) {
	t.Helper()
	in := make(chan runMessage, 1)
	addr = rawWorker(t, func(c net.Conn) {
		var req runMessage
		if readMessage(c, &req) != nil {
			return
		}
		in <- req
		script(c)
	})
	return addr, in
}

func TestRun_ChecksTheRequestBeforeDialing(t *testing.T) {
	for _, addr := range []string{"8.8.8.8:9100", "example.com:9100", "0.0.0.0:9100", "no-port"} {
		_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
		assert.Error(t, err, addr)
	}
	_, err := Run(context.Background(), "127.0.0.1:1", RunRequest{}, askForOneOrder)
	assert.ErrorContains(t, err, "run request")
}

func TestRun_NothingListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	_, err = Run(context.Background(), addr, goodRequest, askForOneOrder)

	assert.Error(t, err)
}

func TestRun_SendsTheRequestAndFollowsTradersMessagesToTheResult(t *testing.T) {
	warm := []marketdata.Bar{{Instrument: "EURUSD", Volume: 1}}
	live := []marketdata.Bar{{Instrument: "EURUSD", Volume: 2}}
	events := []Event{{Kind: Filled, ID: "sim-1", Instrument: "EURUSD", Action: "open", Cause: "strategy"}}
	var replies []ordersMessage
	addr, got := fakeTrader(t, func(c net.Conn) {
		_ = writeMessage(c, startMessage{Accepted: &acceptedRun{RunID: "run_7"}})
		_ = writeMessage(c, barsMessage{Bars: warm, Events: []Event{}, Warmup: true})
		replies = append(replies, readReply(t, c))
		_ = writeMessage(c, barsMessage{Bars: live, Events: events})
		replies = append(replies, readReply(t, c))
		_ = writeMessage(c, resultMessage{Result: Result{RunID: "run_7", Status: StatusCompleted, Summary: []byte(`{"trades":1}`)}})
	})
	var warmed, decided [][]marketdata.Bar
	var seen []Event

	res, err := Run(context.Background(), addr, goodRequest,
		func(bars []marketdata.Bar, ev []Event) ([]Order, error) {
			decided, seen = append(decided, bars), ev
			return []Order{{Instrument: "EURUSD", Action: "close"}}, nil
		},
		WithWarmup(func(bars []marketdata.Bar) { warmed = append(warmed, bars) }))

	require.NoError(t, err)
	assert.Equal(t, goodRequest, (<-got).Run)
	assert.Equal(t, "run_7", res.RunID)
	assert.Equal(t, StatusCompleted, res.Status)
	assert.JSONEq(t, `{"trades":1}`, string(res.Summary))
	assert.Equal(t, [][]marketdata.Bar{warm}, warmed)
	assert.Equal(t, [][]marketdata.Bar{live}, decided, "a warm-up group never reaches onBars")
	assert.Equal(t, events, seen)
	require.Len(t, replies, 2)
	assert.Empty(t, replies[0].Orders, "a warm-up is answered with no orders")
	assert.NotNil(t, replies[0].Orders, "an empty list on the wire, not null")
	assert.Len(t, replies[1].Orders, 1)
}

func readReply(t *testing.T, c net.Conn) ordersMessage {
	t.Helper()
	var r ordersMessage
	require.NoError(t, readMessage(c, &r))
	return r
}

func TestRun_WithoutAWarmupFunctionWarmupBarsAreAnsweredAndIgnored(t *testing.T) {
	var reply ordersMessage
	addr, _ := fakeTrader(t, func(c net.Conn) {
		_ = writeMessage(c, startMessage{Accepted: &acceptedRun{RunID: "run_1"}})
		_ = writeMessage(c, barsMessage{Bars: oneBar, Events: []Event{}, Warmup: true})
		reply = readReply(t, c)
		_ = writeMessage(c, resultMessage{Result: Result{RunID: "run_1", Status: StatusCompleted}})
	})
	calls := 0

	_, err := Run(context.Background(), addr, goodRequest, func([]marketdata.Bar, []Event) ([]Order, error) { calls++; return nil, nil })

	require.NoError(t, err)
	assert.Zero(t, calls)
	assert.NotNil(t, reply.Orders)
}

func TestRun_ARefusalIsAnErrorWithTradersReason(t *testing.T) {
	addr, _ := fakeTrader(t, func(c net.Conn) {
		_ = writeMessage(c, startMessage{Error: "no bars for EURUSD"})
	})

	res, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	assert.ErrorContains(t, err, "the trader refused the run: no bars for EURUSD")
	assert.Nil(t, res)
}

func TestRun_AnAnswerThatIsNeitherAcceptedNorAnErrorIsAnError(t *testing.T) {
	for _, body := range []string{`{}`, `{"accepted":{"run_id":""}}`, `{"accepted":{"run_id":"r"},"x":1}`, `{"orders":[]}`} {
		addr, _ := fakeTrader(t, func(c net.Conn) { writeRaw(c, body) })
		_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
		assert.Error(t, err, body)
	}
}

func TestRun_ProtocolViolationsFromTraderAreErrors(t *testing.T) {
	accepted := startMessage{Accepted: &acceptedRun{RunID: "run_1"}}
	for name, body := range map[string]string{
		"bars with no events list":   `{"bars":[]}`,
		"a result that carries bars": `{"bars":[],"events":[],"result":{"run_id":"r","status":"completed"}}`,
		"a field the protocol lacks": `{"bars":[],"events":[],"extra":1}`,
		"not json":                   `nope`,
	} {
		t.Run(name, func(t *testing.T) {
			addr, _ := fakeTrader(t, func(c net.Conn) {
				_ = writeMessage(c, accepted)
				writeRaw(c, body)
			})
			_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)
			assert.Error(t, err)
		})
	}
}

func TestRun_TraderHangingUpBeforeTheResultIsAnError(t *testing.T) {
	addr, _ := fakeTrader(t, func(c net.Conn) {
		_ = writeMessage(c, startMessage{Accepted: &acceptedRun{RunID: "run_1"}})
	})

	_, err := Run(context.Background(), addr, goodRequest, askForOneOrder)

	assert.ErrorContains(t, err, "closed the connection before the result")
}

func TestRun_AFailingOnBarsEndsTheRunWithItsError(t *testing.T) {
	addr, _ := fakeTrader(t, func(c net.Conn) {
		_ = writeMessage(c, startMessage{Accepted: &acceptedRun{RunID: "run_1"}})
		_ = writeMessage(c, barsMessage{Bars: oneBar, Events: []Event{}})
		var r ordersMessage
		_ = readMessage(c, &r) // EOF: the client hung up
	})

	_, err := Run(context.Background(), addr, goodRequest, func([]marketdata.Bar, []Event) ([]Order, error) {
		return nil, assert.AnError
	})

	assert.ErrorIs(t, err, assert.AnError)
}

func TestRun_CancellingTheContextAbandonsAWaitingRun(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	addr, _ := fakeTrader(t, func(c net.Conn) { <-release }) // never answers

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := Run(ctx, addr, goodRequest, askForOneOrder, WithQueueTimeout(time.Minute))

	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestRun_CancellingTheContextAbandonsAFollowedRun(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	addr, _ := fakeTrader(t, func(c net.Conn) {
		_ = writeMessage(c, startMessage{Accepted: &acceptedRun{RunID: "run_1"}})
		<-release // accepted, then silent
	})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := Run(ctx, addr, goodRequest, askForOneOrder)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestRun_ATraderThatNeverAcceptsIsGivenUpOn(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	addr, _ := fakeTrader(t, func(c net.Conn) { <-release })

	start := time.Now()
	_, err := Run(context.Background(), addr, goodRequest, askForOneOrder, WithQueueTimeout(100*time.Millisecond))

	assert.ErrorContains(t, err, "did not accept the run within 100ms")
	assert.Less(t, time.Since(start), 2*time.Second)
}
