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
// a mutex so -race can actually observe an unserialized send (had
// runSession.send released its mutex before calling Send, this would
// be a data race between concurrent appends).
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

// TestRunSession_SendIsSerialized exercises runSession.send from many
// goroutines concurrently under -race: the fix holds sendMu across
// the actual stream.Send call (not just while reading the stream
// pointer), so this must never race regardless of how many goroutines
// call send concurrently.
func TestRunSession_SendIsSerialized(t *testing.T) {
	descriptor := strategy.Descriptor{Name: "x"}
	sess := newRunSession("sess-1", descriptor, nil, 0)

	stream := &fakeRunStream{}
	require.NoError(t, sess.bindStreamForTest(stream))

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = sess.send(&v1.RunServerMessage{Payload: &v1.RunServerMessage_SessionEnd{
				SessionEnd: &v1.SessionEnd{Reason: string(rune('a' + i%26))},
			}})
		}(i)
	}
	wg.Wait()

	stream.mu.Lock()
	defer stream.mu.Unlock()
	require.Len(t, stream.sent, n)
}

// bindStreamForTest exposes bindStream to this internal test file
// under a distinct name to make clear it is test-only wiring, not a
// second production entry point.
func (s *runSession) bindStreamForTest(stream v1.StrategyHostService_RunServer) error {
	return s.bindStream(stream)
}
