package server

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategy/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exchange runs script as the Handler of one run against a real worker
// connection driven by worker, and returns what the script returned. The worker
// sees accepted first, then whatever the script sends.
func exchange(t *testing.T, cfg Config, script func(ctx context.Context, s *Session) error, worker func(c net.Conn, runID string)) error {
	t.Helper()
	result := make(chan error, 1)
	cfg.Handle = func(ctx context.Context, s *Session) error {
		err := script(ctx, s)
		result <- err
		return err
	}
	r := start(t, cfg)
	c, accepted := r.register(validRegister())
	worker(c, accepted.RunID)
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the handler did not return")
		return nil
	}
}

func reply(runID string, seq, inReplyTo uint64, orders ...strategy.Order) *proto.Orders {
	return &proto.Orders{Header: proto.Header{Seq: seq, RunID: runID}, InReplyTo: inReplyTo, Orders: orders}
}

func TestSession_ExchangeIsStrictLockStepWithSequenceAndRunID(t *testing.T) {
	var runID string
	var got1, got2 *proto.Orders
	err := exchange(t, Config{},
		func(ctx context.Context, s *Session) error {
			var err error
			if got1, err = s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD", "GBPUSD")); err != nil {
				return err
			}
			got2, err = s.Exchange(ctx, sampleBars("2024-03-01T12:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			runID = id
			b1 := recv(t, c).(*proto.Bars)
			assert.Equal(t, proto.Header{V: 1, Type: proto.TypeBars, Seq: 2, RunID: id}, b1.Header, "after accepted, seq 2, the RunID, stamped by the host")
			assert.Equal(t, ts("2024-03-01T11:00:00Z"), b1.FrameAt)
			assert.Len(t, b1.Bars, 2)
			send(t, c, reply(id, 2, 2, goodOrder("EURUSD")))
			b2 := recv(t, c).(*proto.Bars)
			assert.Equal(t, uint64(3), b2.Seq)
			assert.Equal(t, id, b2.RunID)
			send(t, c, reply(id, 3, 3))
			d := recv(t, c).(*proto.Done)
			assert.Equal(t, proto.StatusCompleted, d.Status)
			assert.Equal(t, uint64(4), d.Seq)
			assert.Equal(t, id, d.RunID, "the RunID is on every message")
		})

	require.NoError(t, err)
	require.Len(t, got1.Orders, 1)
	assert.Equal(t, "EURUSD", got1.Orders[0].Instrument)
	assert.Equal(t, "t", got1.Orders[0].Ref)
	assert.Empty(t, got2.Orders)
	assert.NotEmpty(t, runID)
}

func TestSession_TheCallerDoesNotSetTheHeader(t *testing.T) {
	in := sampleBars("2024-03-01T11:00:00Z", "EURUSD")
	in.Header = proto.Header{Seq: 99, RunID: "wrong", Type: proto.TypeDone}
	err := exchange(t, Config{},
		func(ctx context.Context, s *Session) error { _, err := s.Exchange(ctx, in); return err },
		func(c net.Conn, id string) {
			b := recv(t, c).(*proto.Bars)
			assert.Equal(t, proto.Header{V: 1, Type: proto.TypeBars, Seq: 2, RunID: id}, b.Header)
			send(t, c, reply(id, 2, 2))
		})

	require.NoError(t, err)
	assert.Equal(t, uint64(99), in.Seq, "the caller's message is not modified")
}

func TestSession_AccessorsAndGrant(t *testing.T) {
	var s0 *Session
	_ = exchange(t, Config{Admit: func(context.Context, *proto.Register) (Grant, error) {
		return Grant{Instruments: []string{"EURUSD"}, WarmBars: 3}, nil
	}},
		func(_ context.Context, s *Session) error { s0 = s; return nil },
		func(c net.Conn, id string) { recv(t, c) })

	assert.Equal(t, []string{"EURUSD"}, s0.Grant().Instruments)
	assert.Equal(t, 3, s0.Grant().WarmBars)
	assert.Equal(t, "emax", s0.Register().Name)
	assert.NotEmpty(t, s0.RunID())
	assert.True(t, s0.isClosed())
	assert.Equal(t, "completed", s0.status())
}

// The worker's replies that break the protocol or the order rules end the run: the
// worker is told, the connection closes, the handler gets the error.
func TestSession_AReplyThatBreaksTheRulesEndsTheRun(t *testing.T) {
	noStop := goodOrder("EURUSD")
	noStop.StopLoss = num0()
	for name, tc := range map[string]struct {
		msg  func(runID string) proto.Message
		code proto.Code
		raw  string
	}{
		"wrong run_id":              {func(id string) proto.Message { return reply("r-other", 2, 2) }, proto.CodeProtocol, ""},
		"a seq gap":                 {func(id string) proto.Message { return reply(id, 3, 2) }, proto.CodeProtocol, ""},
		"a repeated seq":            {func(id string) proto.Message { return reply(id, 1, 2) }, proto.CodeProtocol, ""},
		"answering the wrong frame": {func(id string) proto.Message { return reply(id, 2, 1) }, proto.CodeProtocol, ""},
		"answering a future frame":  {func(id string) proto.Message { return reply(id, 2, 9) }, proto.CodeProtocol, ""},
		"a register again":          {func(id string) proto.Message { m := validRegister(); m.Seq = 2; return m }, proto.CodeProtocol, ""},
		"a done from the worker": {func(id string) proto.Message {
			return &proto.Done{Header: proto.Header{Seq: 2, RunID: id}, Status: "completed"}
		}, proto.CodeProtocol, ""},
		"an open with no stop":       {func(id string) proto.Message { return reply(id, 2, 2, noStop) }, proto.CodeInvalidOrder, ""},
		"an unregistered instrument": {func(id string) proto.Message { return reply(id, 2, 2, goodOrder("USDJPY")) }, proto.CodeInvalidOrder, ""},
		"a valid and an invalid":     {func(id string) proto.Message { return reply(id, 2, 2, goodOrder("EURUSD"), noStop) }, proto.CodeInvalidOrder, ""},
		"an expiry before the frame": {func(id string) proto.Message {
			o := goodOrder("EURUSD")
			o.Expires = ts("2024-03-01T10:00:00Z")
			return reply(id, 2, 2, o)
		}, proto.CodeInvalidOrder, ""},
		"a host-owned member":     {nil, proto.CodeInvalidOrder, `{"v":1,"type":"orders","seq":2,"run_id":"%s","in_reply_to":2,"orders":[{"instrument":"EURUSD","action":"close","units":1000}]}`},
		"an unknown order member": {nil, proto.CodeInvalidOrder, `{"v":1,"type":"orders","seq":2,"run_id":"%s","in_reply_to":2,"orders":[{"instrument":"EURUSD","action":"close","stoploss":"1.0"}]}`},
		"not JSON":                {nil, proto.CodeProtocol, `garbage%.0s`},
		"another version":         {nil, proto.CodeVersion, `{"v":2,"type":"orders","seq":2,"run_id":"%s"}`},
		"a duplicate member":      {nil, proto.CodeProtocol, `{"v":1,"v":1,"type":"orders","seq":2,"run_id":"%s"}`},
	} {
		t.Run(name, func(t *testing.T) {
			var failure *proto.Failure
			err := exchange(t, Config{},
				func(ctx context.Context, s *Session) error {
					_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
					return err
				},
				func(c net.Conn, id string) {
					recv(t, c)
					if tc.msg != nil {
						send(t, c, tc.msg(id))
					} else {
						sendRaw(t, c, sprintf(tc.raw, id))
					}
					failure = recvError(t, c)
					expectClosed(t, c)
				})

			var pe *proto.Error
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, tc.code, pe.Code)
			assert.Equal(t, tc.code, failure.Code)
			assert.True(t, failure.Fatal)
			assert.Equal(t, uint64(3), failure.Seq, "the error continues Trader's sequence")
		})
	}
}

func TestSession_ARefusedBatchSubmitsNothing(t *testing.T) {
	var orders *proto.Orders
	noStop := goodOrder("EURUSD")
	noStop.StopLoss = num0()
	err := exchange(t, Config{},
		func(ctx context.Context, s *Session) error {
			var err error
			orders, err = s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			recv(t, c)
			send(t, c, reply(id, 2, 2, goodOrder("EURUSD"), noStop))
			recvError(t, c)
		})

	require.Error(t, err)
	assert.Nil(t, orders, "no order of a refused message is handed over")
}

func TestSession_AWarmUpFrameTakesNoOrders(t *testing.T) {
	warm := sampleBars("2024-02-29T11:00:00Z", "EURUSD")
	warm.Warm = true
	t.Run("an empty reply is fine", func(t *testing.T) {
		err := exchange(t, Config{},
			func(ctx context.Context, s *Session) error { _, err := s.Exchange(ctx, warm); return err },
			func(c net.Conn, id string) {
				b := recv(t, c).(*proto.Bars)
				assert.True(t, b.Warm)
				send(t, c, reply(id, 2, 2))
			})
		require.NoError(t, err)
	})
	t.Run("an order is refused", func(t *testing.T) {
		err := exchange(t, Config{},
			func(ctx context.Context, s *Session) error { _, err := s.Exchange(ctx, warm); return err },
			func(c net.Conn, id string) {
				recv(t, c)
				send(t, c, reply(id, 2, 2, goodOrder("EURUSD")))
				assert.Equal(t, proto.CodeInvalidOrder, recvError(t, c).Code)
			})
		assert.Equal(t, proto.CodeInvalidOrder, proto.CodeOf(err))
	})
}

func TestSession_ASlowReplyTimesOut(t *testing.T) {
	var failure *proto.Failure
	err := exchange(t, Config{ReplyTimeout: 150 * time.Millisecond},
		func(ctx context.Context, s *Session) error {
			_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			recv(t, c) // the bars, then silence
			failure = recvError(t, c)
			expectClosed(t, c)
		})

	assert.Equal(t, proto.CodeTimeout, proto.CodeOf(err))
	assert.Equal(t, proto.CodeTimeout, failure.Code)
}

func TestSession_ADisconnectFailsTheRunClosed(t *testing.T) {
	for name, hangUp := range map[string]func(c net.Conn){
		"after reading the bars": func(c net.Conn) { recv(t, c); _ = c.Close() },
		"in the middle of the reply": func(c net.Conn) {
			recv(t, c)
			_, _ = c.Write([]byte{0, 0, 0, 50, '{'})
			_ = c.Close()
		},
	} {
		t.Run(name, func(t *testing.T) {
			var s0 *Session
			err := exchange(t, Config{},
				func(ctx context.Context, s *Session) error {
					s0 = s
					_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
					return err
				},
				func(c net.Conn, id string) { hangUp(c) })

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDisconnected) || proto.CodeOf(err) == proto.CodeProtocol, "%v", err)
			assert.True(t, s0.isClosed())
			_, again := s0.Exchange(context.Background(), sampleBars("2024-03-01T12:00:00Z", "EURUSD"))
			assert.ErrorIs(t, again, ErrClosed)
		})
	}
}

func TestSession_ADisconnectBeforeTheBarsAreWrittenIsReported(t *testing.T) {
	err := exchange(t, Config{},
		func(ctx context.Context, s *Session) error {
			time.Sleep(200 * time.Millisecond) // the worker is gone by now
			for range 50 {
				if _, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD")); err != nil {
					return err
				}
			}
			return nil
		},
		func(c net.Conn, id string) { _ = c.Close() })

	require.ErrorIs(t, err, ErrDisconnected)
}

func TestSession_AWorkerErrorEndsTheRun(t *testing.T) {
	err := exchange(t, Config{},
		func(ctx context.Context, s *Session) error {
			_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			recv(t, c)
			send(t, c, &proto.Failure{Header: proto.Header{Seq: 2, RunID: id}, Code: proto.CodeInternal, Fatal: true, Message: "strategy panicked"})
			expectClosed(t, c)
		})

	var we *WorkerError
	require.ErrorAs(t, err, &we)
	assert.Equal(t, "strategy panicked", we.Failure.Message)
	assert.Contains(t, err.Error(), "strategy panicked")
}

func TestSession_CancellingTheContextEndsTheRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var d *proto.Done
	err := exchange(t, Config{},
		func(_ context.Context, s *Session) error {
			time.AfterFunc(100*time.Millisecond, cancel)
			_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			recv(t, c) // never answered
			d = recv(t, c).(*proto.Done)
			expectClosed(t, c)
		})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, proto.StatusCancelled, d.Status)
}

func TestSession_ACancelledContextBeforeTheCallSendsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := exchange(t, Config{},
		func(_ context.Context, s *Session) error {
			_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			d := recv(t, c).(*proto.Done)
			assert.Equal(t, proto.StatusCancelled, d.Status, "the first thing the worker sees is done")
		})

	require.ErrorIs(t, err, context.Canceled)
}

func TestSession_FinishIsOnceAndValidatesTheStatus(t *testing.T) {
	var first, second, bad error
	_ = exchange(t, Config{},
		func(_ context.Context, s *Session) error {
			bad = s.Finish("ok", "")
			first = s.Finish(proto.StatusFailed, "stopped")
			second = s.Finish(proto.StatusCompleted, "")
			return nil
		},
		func(c net.Conn, id string) {
			d := recv(t, c).(*proto.Done)
			assert.Equal(t, proto.StatusFailed, d.Status)
			assert.Equal(t, "stopped", d.Reason)
			expectClosed(t, c)
		})

	assert.Error(t, bad)
	assert.NoError(t, first)
	assert.ErrorIs(t, second, ErrClosed)
}

func TestSession_FailSendsTheErrorAndClosesOnce(t *testing.T) {
	var first, second, unknownCode error
	err := exchange(t, Config{},
		func(ctx context.Context, s *Session) error {
			first = s.Fail(proto.CodeInternal, "engine stopped")
			second = s.Fail(proto.CodeInternal, "again")
			_, unknownCode = s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return nil
		},
		func(c net.Conn, id string) {
			f := recvError(t, c)
			assert.Equal(t, proto.CodeInternal, f.Code)
			assert.Equal(t, "engine stopped", f.Message)
			assert.Equal(t, id, f.RunID)
			expectClosed(t, c)
		})

	require.NoError(t, err)
	assert.NoError(t, first)
	assert.ErrorIs(t, second, ErrClosed)
	assert.ErrorIs(t, unknownCode, ErrClosed)
}

func TestSession_FailWithAnUnknownCodeIsInternal(t *testing.T) {
	_ = exchange(t, Config{},
		func(_ context.Context, s *Session) error { return s.Fail("nonsense", "x") },
		func(c net.Conn, id string) { assert.Equal(t, proto.CodeInternal, recvError(t, c).Code) })
}

func TestSession_ABarsMessageTheCodecRefusesIsTheHostsBug(t *testing.T) {
	big := sampleBars("2024-03-01T11:00:00Z", "EURUSD", "EURUSD", "EURUSD", "EURUSD")
	err := exchange(t, Config{MaxFrame: 700},
		func(ctx context.Context, s *Session) error { _, err := s.Exchange(ctx, big); return err },
		func(c net.Conn, id string) {})

	assert.Equal(t, proto.CodeInternal, proto.CodeOf(err), "a frame over the host's own limit is not the worker's fault")
}

func TestSession_OversizedReplyIsAProtocolError(t *testing.T) {
	err := exchange(t, Config{MaxFrame: 1 << 12},
		func(ctx context.Context, s *Session) error {
			_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		},
		func(c net.Conn, id string) {
			recv(t, c)
			_, werr := c.Write([]byte{0, 1, 0, 0}) // 65536 bytes promised, over the 4096 limit
			require.NoError(t, werr)
			assert.Equal(t, proto.CodeProtocol, recvError(t, c).Code)
		})

	assert.Equal(t, proto.CodeProtocol, proto.CodeOf(err))
}
