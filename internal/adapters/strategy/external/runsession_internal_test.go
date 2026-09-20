package external

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/rustyeddy/trader/internal/strategy"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// fakeRunStream is a minimal v1.StrategyHostService_RunServer double
// that records every Send call's own message, guarding its slice with
// a mutex so -race can actually observe an unserialized send.
type fakeRunStream struct {
	mu   sync.Mutex
	sent []*v1.RunServerMessage
}

func (f *fakeRunStream) Send(msg *v1.RunServerMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}
func (f *fakeRunStream) Recv() (*v1.RunClientMessage, error) { return nil, nil }
func (f *fakeRunStream) Context() context.Context            { return context.Background() }
func (f *fakeRunStream) SendMsg(m any) error                 { return nil }
func (f *fakeRunStream) RecvMsg(m any) error                 { return nil }
func (f *fakeRunStream) SetHeader(metadata.MD) error         { return nil }
func (f *fakeRunStream) SendHeader(metadata.MD) error        { return nil }
func (f *fakeRunStream) SetTrailer(metadata.MD)              {}

// TestRunSession_SysSendIsSerialized exercises runSession.sysSend from
// many goroutines concurrently under -race: only the dedicated writer
// goroutine (started by run once bind succeeds) ever calls the
// stream's own Send, so concurrent sysSend callers can never race on
// it regardless of how many call at once — the actor model's own
// replacement for a send mutex.
func TestRunSession_SysSendIsSerialized(t *testing.T) {
	descriptor := strategy.Descriptor{Name: "x"}
	sess := newRunSession("sess-1", descriptor, nil, 0)

	stream := &fakeRunStream{}
	require.NoError(t, sess.bind(stream))

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			msg := &v1.RunServerMessage{Payload: &v1.RunServerMessage_SessionEnd{
				SessionEnd: &v1.SessionEnd{Reason: string(rune('a' + i%26))},
			}}
			_ = sess.sysSend(context.Background(), msg)
		}(i)
	}
	wg.Wait()

	stream.mu.Lock()
	defer stream.mu.Unlock()
	require.Len(t, stream.sent, n)
}

// blockingRunStream is a fakeRunStream whose Send blocks until told
// to proceed — used to simulate a guest that has stopped reading
// (stream flow control stalling Send indefinitely).
type blockingRunStream struct {
	fakeRunStream
	release chan struct{}
}

func newBlockingRunStream() *blockingRunStream {
	return &blockingRunStream{release: make(chan struct{})}
}

func (f *blockingRunStream) Send(msg *v1.RunServerMessage) error {
	<-f.release
	return f.fakeRunStream.Send(msg)
}

// TestRunSession_ActorStaysResponsiveDuringBlockedSysSend is the
// review's own regression: run's earlier sysSendCh case waited
// synchronously (`writeCh <- ...; <-result`) inline in the actor's
// own top-level select, so a stalled guest blocking Send blocked the
// *entire actor* — teardown included, which could deadlock Close/Run
// shutdown entirely. handleSysSend fixes this by keeping run's own
// select loop live while a send is outstanding; this test proves a
// concurrent forceTeardown still completes promptly even while a
// sysSend's own Send call is deliberately left blocked.
func TestRunSession_ActorStaysResponsiveDuringBlockedSysSend(t *testing.T) {
	descriptor := strategy.Descriptor{Name: "x"}
	sess := newRunSession("sess-1", descriptor, nil, 0)

	stream := newBlockingRunStream()
	require.NoError(t, sess.bind(stream))

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- sess.sysSend(context.Background(), &v1.RunServerMessage{
			Payload: &v1.RunServerMessage_SessionStart{SessionStart: &v1.SessionStart{RunId: "r"}},
		})
	}()

	// Give sysSend time to actually reach the writer and block in Send.
	time.Sleep(20 * time.Millisecond)

	teardownDone := make(chan struct{})
	go func() {
		sess.forceTeardown(nil)
		close(teardownDone)
	}()

	select {
	case <-teardownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("forceTeardown did not complete promptly while a sysSend was blocked in Send — the actor is wedged")
	}

	close(stream.release) // let the blocked Send finally return
	err := <-sendDone
	require.Error(t, err) // the session ended while this send was outstanding
}

// TestRunSession_SysSendHonorsCallerCtxWhileBlocked is the review's
// own follow-up finding: an earlier fix observed req.ctx only while
// enqueueing the sysSend request, so a caller ctx that ended while
// the actual Send call was still blocked (guest stopped reading)
// could not interrupt it — Start/Close could hang past their own
// caller's deadline. This proves sysSend now returns promptly on
// ctx cancellation even while the underlying Send is deliberately
// left blocked, and that doing so ends the session (ambiguous
// delivery state), exactly like a callback's own ctx cancellation.
func TestRunSession_SysSendHonorsCallerCtxWhileBlocked(t *testing.T) {
	descriptor := strategy.Descriptor{Name: "x"}
	sess := newRunSession("sess-1", descriptor, nil, 0)

	stream := newBlockingRunStream()
	require.NoError(t, sess.bind(stream))
	defer close(stream.release) // let the eventually-abandoned Send return

	ctx, cancel := context.WithCancel(context.Background())

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- sess.sysSend(ctx, &v1.RunServerMessage{
			Payload: &v1.RunServerMessage_SessionStart{SessionStart: &v1.SessionStart{RunId: "r"}},
		})
	}()

	time.Sleep(20 * time.Millisecond) // let sysSend actually reach the writer and block in Send
	cancel()

	select {
	case err := <-sendDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("sysSend did not observe caller ctx cancellation while Send was blocked")
	}

	// The session itself must now be over too (ambiguous delivery
	// state, same reasoning as a callback's own ctx cancellation).
	select {
	case <-sess.done:
	case <-time.After(2 * time.Second):
		t.Fatal("session did not end after sysSend's own ctx cancellation")
	}
}

// TestRunSession_BindTwiceRejected proves a second bind on an
// already-bound session is rejected rather than silently replacing
// the stream — v1's own "exactly one Run stream per Handshake'd
// connection" scope statement.
func TestRunSession_BindTwiceRejected(t *testing.T) {
	descriptor := strategy.Descriptor{Name: "x"}
	sess := newRunSession("sess-1", descriptor, nil, 0)

	require.NoError(t, sess.bind(&fakeRunStream{}))
	require.Error(t, sess.bind(&fakeRunStream{}))
}
