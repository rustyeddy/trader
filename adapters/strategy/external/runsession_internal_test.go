package external

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
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
