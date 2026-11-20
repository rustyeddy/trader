package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategy/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rig is a running Server on a loopback port, with a worker's view of it.
type rig struct {
	t      *testing.T
	addr   string
	cancel context.CancelFunc
	done   chan struct{} // closed when Serve has returned
	err    error         // what Serve returned, valid once done is closed
	srv    *Server
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func admitAll(context.Context, *proto.Register) (Grant, error) { return Grant{}, nil }

func idleHandler(ctx context.Context, _ *Session) error { return nil }

func start(t *testing.T, cfg Config) *rig {
	t.Helper()
	if cfg.Admit == nil {
		cfg.Admit = admitAll
	}
	if cfg.Handle == nil {
		cfg.Handle = idleHandler
	}
	if cfg.Logger == nil {
		cfg.Logger = quiet
	}
	if cfg.CloseGrace == 0 {
		cfg.CloseGrace = 20 * time.Millisecond
	}
	srv, err := New(cfg)
	require.NoError(t, err)
	ln, err := Listen("127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{t: t, addr: ln.Addr().String(), cancel: cancel, done: make(chan struct{}), srv: srv}
	go func() { r.err = srv.Serve(ctx, ln); close(r.done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
			t.Error("the server did not stop")
		}
	})
	return r
}

func (r *rig) dial() net.Conn {
	r.t.Helper()
	c, err := net.Dial("tcp", r.addr)
	require.NoError(r.t, err)
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}

func validRegister() *proto.Register {
	return &proto.Register{Header: proto.Header{Seq: 1}, Name: "emax", Version: "0.3", Build: "git:abc",
		Capabilities: []string{"bars", "warm"},
		Run: proto.Run{Mode: proto.ModeBacktest, Interval: "H1", Instruments: []string{"EURUSD", "GBPUSD"},
			From: ts("2024-03-01T00:00:00Z"), To: ts("2024-04-01T00:00:00Z"), WarmBars: 10}}
}

func send(t *testing.T, c net.Conn, m proto.Message) {
	t.Helper()
	require.NoError(t, c.SetWriteDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, proto.Write(c, m, proto.DefaultMaxFrame))
}

func sendRaw(t *testing.T, c net.Conn, payload string) {
	t.Helper()
	require.NoError(t, proto.WriteFrame(c, []byte(payload), proto.DefaultMaxFrame))
}

func recv(t *testing.T, c net.Conn) proto.Message {
	t.Helper()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	m, err := proto.Read(c, proto.DefaultMaxFrame)
	require.NoError(t, err)
	return m
}

func recvError(t *testing.T, c net.Conn) *proto.Failure {
	t.Helper()
	f, ok := recv(t, c).(*proto.Failure)
	require.True(t, ok, "expected an error message")
	return f
}

// expectClosed asserts the server has closed the connection.
func expectClosed(t *testing.T, c net.Conn) {
	t.Helper()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err := proto.ReadFrame(c, proto.DefaultMaxFrame)
	require.ErrorIs(t, err, io.EOF, "the connection is closed")
}

// register dials, registers and returns the connection and the accepted reply.
func (r *rig) register(reg *proto.Register) (net.Conn, *proto.Accepted) {
	r.t.Helper()
	c := r.dial()
	send(r.t, c, reg)
	a, ok := recv(r.t, c).(*proto.Accepted)
	require.True(r.t, ok, "expected accepted")
	return c, a
}

func TestNew_NeedsAnAdmitterAndAHandlerAndSetsDefaults(t *testing.T) {
	_, err := New(Config{Handle: idleHandler})
	require.Error(t, err)
	_, err = New(Config{Admit: admitAll})
	require.Error(t, err)

	s, err := New(Config{Admit: admitAll, Handle: idleHandler})

	require.NoError(t, err)
	assert.Equal(t, DefaultRegisterTimeout, s.cfg.RegisterTimeout)
	assert.Equal(t, DefaultReplyTimeout, s.cfg.ReplyTimeout)
	assert.Equal(t, time.Second, s.cfg.CloseGrace)
	assert.Equal(t, proto.DefaultMaxFrame, s.cfg.MaxFrame)
	assert.Equal(t, proto.DefaultMaxFrame, s.cfg.MaxFrame)
	assert.NotNil(t, s.cfg.NewRunID)
	assert.NotNil(t, s.log)
}

func TestDefaults_AreTheOnesOfTheADR(t *testing.T) {
	assert.Equal(t, 10*time.Second, DefaultRegisterTimeout)
	assert.Equal(t, 30*time.Second, DefaultReplyTimeout)
}

func TestServer_AcceptsARegistrationAndIssuesARunID(t *testing.T) {
	seen := make(chan *Session, 1)
	var admitted *proto.Register
	r := start(t, Config{
		ReplyTimeout: 7 * time.Second,
		Admit: func(_ context.Context, reg *proto.Register) (Grant, error) {
			admitted = reg
			return Grant{}, nil
		},
		Handle: func(_ context.Context, s *Session) error { seen <- s; return nil },
	})

	conn, accepted := r.register(validRegister())

	assert.Equal(t, proto.Header{V: 1, Type: proto.TypeAccepted, Seq: 1, RunID: accepted.RunID}, accepted.Header)
	assert.Regexp(t, regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`), accepted.RunID)
	assert.Equal(t, []string{"EURUSD", "GBPUSD"}, accepted.Instruments, "an empty grant means what the worker asked for")
	assert.Equal(t, "H1", accepted.Interval)
	assert.Zero(t, accepted.WarmBars, "the host grants warm-up; it is not echoed from the request")
	assert.Equal(t, 7000, accepted.ReplyTimeoutMS)
	require.NotNil(t, admitted)
	assert.Equal(t, "emax", admitted.Name)
	assert.Equal(t, "git:abc", admitted.Build)

	s := <-seen
	assert.Equal(t, accepted.RunID, s.RunID(), "the run context carries the same RunID")
	assert.Equal(t, "emax", s.Register().Name)
	done := recv(t, conn).(*proto.Done)
	assert.Equal(t, proto.StatusCompleted, done.Status, "a handler that returns nil completes the run")
	assert.Equal(t, accepted.RunID, done.RunID)
	assert.Equal(t, uint64(2), done.Seq)
}

func TestServer_TheGrantDecidesInstrumentsAndWarmUp(t *testing.T) {
	r := start(t, Config{Admit: func(context.Context, *proto.Register) (Grant, error) {
		return Grant{Instruments: []string{"GBPUSD"}, WarmBars: 4}, nil
	}})

	_, accepted := r.register(validRegister())

	assert.Equal(t, []string{"GBPUSD"}, accepted.Instruments)
	assert.Equal(t, 4, accepted.WarmBars)
}

func TestServer_RefusesWhatTheHostRefusesAndNeverStartsTheRun(t *testing.T) {
	called := false
	r := start(t, Config{
		Admit: func(context.Context, *proto.Register) (Grant, error) {
			return Grant{}, Reject("instrument %s is not available", "XYZ")
		},
		Handle: func(context.Context, *Session) error { called = true; return nil },
	})
	c := r.dial()
	send(t, c, validRegister())

	f := recvError(t, c)

	assert.Equal(t, proto.CodeRejected, f.Code)
	assert.True(t, f.Fatal)
	assert.Contains(t, f.Message, "instrument XYZ is not available")
	assert.Empty(t, f.RunID, "no run exists")
	assert.Equal(t, uint64(1), f.Seq)
	expectClosed(t, c)
	assert.False(t, called)
}

func TestServer_AHostFailureWhileAdmittingIsInternalAndLeaksNothing(t *testing.T) {
	r := start(t, Config{Admit: func(context.Context, *proto.Register) (Grant, error) {
		return Grant{}, errors.New("database password is hunter2")
	}})
	c := r.dial()
	send(t, c, validRegister())

	f := recvError(t, c)

	assert.Equal(t, proto.CodeInternal, f.Code)
	assert.NotContains(t, f.Message, "hunter2")
	expectClosed(t, c)
}

func TestServer_ARegistrationThatFailsValidationNeverReachesTheAdmitter(t *testing.T) {
	admitted := false
	r := start(t, Config{Admit: func(context.Context, *proto.Register) (Grant, error) { admitted = true; return Grant{}, nil }})
	for name, edit := range map[string]func(*proto.Register){
		"live is reserved":      func(m *proto.Register) { m.Run.Mode = proto.ModeLive },
		"no bars capability":    func(m *proto.Register) { m.Capabilities = []string{"warm"} },
		"an end before a start": func(m *proto.Register) { m.Run.To = m.Run.From.Add(-time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			c := r.dial()
			m := validRegister()
			edit(m)
			send(t, c, m)

			assert.Equal(t, proto.CodeRejected, recvError(t, c).Code)
			expectClosed(t, c)
		})
	}
	assert.False(t, admitted)
}

func TestServer_ARunObjectWithAnUnknownMemberIsRejected(t *testing.T) {
	r := start(t, Config{})
	c := r.dial()
	sendRaw(t, c, `{"v":1,"type":"register","seq":1,"name":"x","capabilities":["bars"],"run":{"mode":"backtest","interval":"H1","instrument":["EURUSD"],"from":"2024-03-01T00:00:00Z","to":"2024-04-01T00:00:00Z"}}`)

	f := recvError(t, c)

	assert.Equal(t, proto.CodeRejected, f.Code)
	assert.Contains(t, f.Message, `"instrument"`)
}

func TestServer_ProtocolViolationsBeforeAcceptedAreErrorsThenClose(t *testing.T) {
	r := start(t, Config{})
	zeroLen := func(c net.Conn) {
		_, err := c.Write([]byte{0, 0, 0, 0})
		require.NoError(t, err)
	}
	huge := func(c net.Conn) {
		_, err := c.Write(binary.BigEndian.AppendUint32(nil, 0xFFFFFFFF))
		require.NoError(t, err)
	}
	for name, tc := range map[string]struct {
		do   func(c net.Conn)
		code proto.Code
	}{
		"wrong version": {func(c net.Conn) { sendRaw(t, c, `{"v":2,"type":"register","seq":1}`) }, proto.CodeVersion},
		"not a register": {func(c net.Conn) {
			send(t, c, &proto.Done{Header: proto.Header{Seq: 1, RunID: "r"}, Status: "completed"})
		}, proto.CodeProtocol},
		"a register with seq 2": {func(c net.Conn) { m := validRegister(); m.Seq = 2; send(t, c, m) }, proto.CodeProtocol},
		"not JSON":              {func(c net.Conn) { sendRaw(t, c, `hello`) }, proto.CodeProtocol},
		"duplicate member":      {func(c net.Conn) { sendRaw(t, c, `{"v":1,"v":1}`) }, proto.CodeProtocol},
		"zero length frame":     {zeroLen, proto.CodeProtocol},
		"a hostile length":      {huge, proto.CodeProtocol},
		"a register with a run_id": {func(c net.Conn) {
			sendRaw(t, c, `{"v":1,"type":"register","seq":1,"run_id":"r","name":"x","capabilities":["bars"],"run":{}}`)
		}, proto.CodeProtocol},
	} {
		t.Run(name, func(t *testing.T) {
			c := r.dial()
			tc.do(c)

			f := recvError(t, c)

			assert.Equal(t, tc.code, f.Code)
			assert.True(t, f.Fatal)
			expectClosed(t, c)
		})
	}
}

func TestServer_ARegistrationThatNeverComesTimesOut(t *testing.T) {
	r := start(t, Config{RegisterTimeout: 150 * time.Millisecond})
	c := r.dial()

	f := recvError(t, c)

	assert.Equal(t, proto.CodeTimeout, f.Code)
	expectClosed(t, c)
}

func TestServer_AHalfSentRegisterTimesOutToo(t *testing.T) {
	r := start(t, Config{RegisterTimeout: 150 * time.Millisecond})
	c := r.dial()
	_, err := c.Write([]byte{0, 0, 1, 0, '{'}) // promises 256 bytes, sends one

	require.NoError(t, err)
	assert.Equal(t, proto.CodeTimeout, recvError(t, c).Code)
}

func TestServer_AConnectionThatHangsUpFirstIsForgottenAndTheServerGoesOn(t *testing.T) {
	r := start(t, Config{})
	c := r.dial()
	require.NoError(t, c.Close())
	half := r.dial()
	_, err := half.Write([]byte{0, 0})
	require.NoError(t, err)
	require.NoError(t, half.Close())

	_, accepted := r.register(validRegister())

	assert.NotEmpty(t, accepted.RunID)
}

func TestServer_EveryRunGetsItsOwnRunID(t *testing.T) {
	r := start(t, Config{})
	ids := map[string]bool{}
	for range 5 {
		_, a := r.register(validRegister())
		ids[a.RunID] = true
	}

	assert.Len(t, ids, 5)
}

func TestServer_RunsAreIndependentAndConcurrent(t *testing.T) {
	var mu sync.Mutex
	in := 0
	release := make(chan struct{})
	r := start(t, Config{Handle: func(ctx context.Context, s *Session) error {
		mu.Lock()
		in++
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}})
	c1, a1 := r.register(validRegister())
	c2, a2 := r.register(validRegister())
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return in == 2 }, 5*time.Second, 10*time.Millisecond)

	close(release)

	assert.NotEqual(t, a1.RunID, a2.RunID)
	assert.Equal(t, a1.RunID, recv(t, c1).(*proto.Done).RunID)
	assert.Equal(t, a2.RunID, recv(t, c2).(*proto.Done).RunID)
}

func TestServer_TheRunIDGeneratorIsInjectable(t *testing.T) {
	r := start(t, Config{NewRunID: func() (string, error) { return "r-fixed", nil }})

	_, a := r.register(validRegister())

	assert.Equal(t, "r-fixed", a.RunID)
}

func TestServer_AFailureToIssueARunIDIsInternal(t *testing.T) {
	r := start(t, Config{NewRunID: func() (string, error) { return "", errors.New("entropy") }})
	c := r.dial()
	send(t, c, validRegister())

	assert.Equal(t, proto.CodeInternal, recvError(t, c).Code)
	expectClosed(t, c)
}

func TestServer_HandlerOutcomesBecomeDone(t *testing.T) {
	for name, tc := range map[string]struct {
		handle func(context.Context, *Session) error
		status string
		reason string
	}{
		"nil completes":               {func(context.Context, *Session) error { return nil }, proto.StatusCompleted, ""},
		"an error fails":              {func(context.Context, *Session) error { return errors.New("engine stopped: bad bar") }, proto.StatusFailed, "engine stopped: bad bar"},
		"the handler finishes itself": {func(_ context.Context, s *Session) error { return s.Finish(proto.StatusCancelled, "owner halt") }, proto.StatusCancelled, "owner halt"},
	} {
		t.Run(name, func(t *testing.T) {
			r := start(t, Config{Handle: tc.handle})
			c, _ := r.register(validRegister())

			d := recv(t, c).(*proto.Done)

			assert.Equal(t, tc.status, d.Status)
			assert.Equal(t, tc.reason, d.Reason)
			expectClosed(t, c)
		})
	}
}

func TestServer_ShuttingDownCancelsRunsAndTellsTheWorker(t *testing.T) {
	started := make(chan struct{})
	r := start(t, Config{Handle: func(ctx context.Context, s *Session) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	c, _ := r.register(validRegister())
	<-started

	r.cancel()

	d := recv(t, c).(*proto.Done)
	assert.Equal(t, proto.StatusCancelled, d.Status)
	select {
	case <-r.done:
		assert.NoError(t, r.err, "a shutdown is not an error")
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestServer_StopsAcceptingAfterShutdown(t *testing.T) {
	r := start(t, Config{})
	r.cancel()
	<-r.done

	_, err := net.DialTimeout("tcp", r.addr, time.Second)

	assert.Error(t, err)
}

func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:9000": true, "127.9.9.9:1": true, "[::1]:9000": true, "localhost:9000": true,
		"0.0.0.0:9000": false, ":9000": false, "[::]:9000": false, "192.168.1.5:9000": false,
		"10.0.0.1:1": false, "example.com:80": false, "8.8.8.8:53": false,
	} {
		got, err := IsLoopbackAddr(addr)
		require.NoError(t, err, addr)
		assert.Equal(t, want, got, addr)
	}
	for _, bad := range []string{"", "127.0.0.1", "nonsense"} {
		_, err := IsLoopbackAddr(bad)
		assert.Error(t, err, bad)
	}
}

func TestListen_RefusesAnythingButLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0", "192.168.1.5:0", "example.com:0"} {
		ln, err := Listen(addr)

		require.Error(t, err, addr)
		assert.Nil(t, ln, addr)
	}
	_, err := Listen("not-an-address")
	assert.Error(t, err)

	ln, err := Listen("127.0.0.1:0")
	require.NoError(t, err)
	assert.True(t, ln.Addr().(*net.TCPAddr).IP.IsLoopback())
	require.NoError(t, ln.Close())
}

func TestServe_RefusesAListenerThatIsNotLoopbackWhoeverOpenedIt(t *testing.T) {
	s, err := New(Config{Admit: admitAll, Handle: idleHandler, Logger: quiet})
	require.NoError(t, err)
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = s.Serve(ctx, ln)

	require.Error(t, err, "a listener that is not loopback must be refused, not served")
	assert.Contains(t, err.Error(), "loopback")
	_, derr := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond)
	assert.Error(t, derr, "Serve closed the listener it refused")
}

func TestServe_RefusesANonTCPListener(t *testing.T) {
	s, err := New(Config{Admit: admitAll, Handle: idleHandler, Logger: quiet})
	require.NoError(t, err)
	ln, err := net.Listen("unix", t.TempDir()+"/s.sock")
	require.NoError(t, err)

	assert.Error(t, s.Serve(context.Background(), ln))
}

func TestReject_WrapsErrRejected(t *testing.T) {
	err := Reject("no %s", "room")

	assert.ErrorIs(t, err, ErrRejected)
	assert.Contains(t, err.Error(), "no room")
}

// sampleBars is a bars message for a frame, for the exchange tests.
func sampleBars(frame string, instruments ...string) *proto.Bars {
	b := &proto.Bars{FrameAt: ts(frame)}
	for _, in := range instruments {
		b.Bars = append(b.Bars, marketdata.Bar{Instrument: in, Time: ts(frame).Add(-time.Hour), AvailableAt: ts(frame),
			Mid: marketdata.OHLC{Open: num.MustParsePrice("1.1"), High: num.MustParsePrice("1.1"), Low: num.MustParsePrice("1.1"), Close: num.MustParsePrice("1.1")}})
	}
	return b
}

func goodOrder(instrument string) strategy.Order {
	return strategy.Order{Instrument: instrument, Action: strategy.Open, Direction: strategy.Long, Type: strategy.Market,
		Price: num.MustParsePrice("1.1"), StopLoss: num.MustParsePrice("1.09"), Ref: "t"}
}

func num0() num.Price { return num.Price{} }

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// importsOf lists the module-internal imports of the non-test Go files of dir.
func importsOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	out := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			out[file] = append(out[file], path)
		}
	}
	return out
}

// TestServer_ImportsOnlyTheStandardLibraryAndTheProtocol keeps the host a leaf: it
// knows nothing of the engine, an account, a broker, a provider or the store.
func TestServer_ImportsOnlyTheStandardLibraryAndTheProtocol(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/strategy": true, mod + "/strategy/proto": true}
	imports := importsOf(t, ".")
	require.NotEmpty(t, imports)
	for file, paths := range imports {
		for _, path := range paths {
			if path == mod || strings.HasPrefix(path, mod+"/") {
				assert.True(t, allowed[path], "%s imports %s", file, path)
			}
		}
	}
}

// TestServer_NoGuestPackageImportsTheHost keeps a guest from linking host code:
// strategy, strategy/proto and strategy/client must not import strategy/server.
func TestServer_NoGuestPackageImportsTheHost(t *testing.T) {
	const host = "github.com/rustyeddy/trader/strategy/server"
	checked := 0
	for _, dir := range []string{"..", filepath.Join("..", "proto"), filepath.Join("..", "client")} {
		for file, paths := range importsOf(t, dir) {
			checked++
			assert.NotContains(t, paths, host, file)
		}
	}
	require.Positive(t, checked)
}

// failingListener accepts its first n connections from the real listener, then
// fails Accept as a broken listener would.
type failingListener struct {
	net.Listener
	mu sync.Mutex
	n  int
}

func (f *failingListener) Accept() (net.Conn, error) {
	f.mu.Lock()
	ok := f.n > 0
	f.n--
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("accept broke")
	}
	return f.Listener.Accept()
}

func TestServer_AListenerFailureDoesNotWaitForASilentWorker(t *testing.T) {
	handling := make(chan struct{})
	srv, err := New(Config{Admit: admitAll, Logger: quiet, ReplyTimeout: time.Minute, CloseGrace: 20 * time.Millisecond,
		Handle: func(ctx context.Context, s *Session) error {
			close(handling)
			_, err := s.Exchange(ctx, sampleBars("2024-03-01T11:00:00Z", "EURUSD"))
			return err
		}})
	require.NoError(t, err)
	ln, err := Listen("127.0.0.1:0")
	require.NoError(t, err)

	served := make(chan error, 1)
	go func() { served <- srv.Serve(context.Background(), &failingListener{Listener: ln, n: 1}) }()

	c, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	send(t, c, validRegister())
	recv(t, c) // accepted
	select {
	case <-handling:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never started")
	}
	recv(t, c) // bars; the worker now stays silent

	select {
	case err := <-served:
		assert.ErrorContains(t, err, "accept")
	case <-time.After(5 * time.Second):
		t.Fatal("Serve waited for the silent worker instead of cancelling it")
	}
}
