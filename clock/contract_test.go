package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contractCase builds a fresh Clock plus a function that advances it by at
// least d: a real sleep for Real, a direct Advance call for Simulated.
// Building a fresh clock per case, rather than sharing one across subtests,
// keeps every test isolated from state left behind by an earlier one.
type contractCase struct {
	name string
	new  func() (Clock, func(time.Duration))
}

var contractCases = []contractCase{
	{
		name: "Real",
		new: func() (Clock, func(time.Duration)) {
			return Real{}, func(d time.Duration) { time.Sleep(d) }
		},
	},
	{
		name: "Simulated",
		new: func() (Clock, func(time.Duration)) {
			sim := NewSimulated(time.Now())
			// Every contract test advances by a positive duration, so
			// Advance's only error case (a negative duration) cannot occur
			// here; see simulated_test.go for that behavior.
			return sim, func(d time.Duration) { _ = sim.Advance(d) }
		},
	},
}

// TestClockContract runs the same suite against every Clock implementation,
// covering only the behavior ADR-015 says both genuinely provide. Exact
// timing, deadline arithmetic, and equal-deadline ordering are
// simulated-clock-only guarantees, tested only in simulated_test.go.
func TestClockContract(t *testing.T) {
	t.Run("Now returns a valid instant", func(t *testing.T) {
		for _, tc := range contractCases {
			t.Run(tc.name, func(t *testing.T) {
				clk, _ := tc.new()
				assert.False(t, clk.Now().IsZero())
			})
		}
	})

	t.Run("a timer eventually becomes ready", func(t *testing.T) {
		for _, tc := range contractCases {
			t.Run(tc.name, func(t *testing.T) {
				clk, advance := tc.new()
				timer := clk.NewTimer(10 * time.Millisecond)
				advance(20 * time.Millisecond)

				select {
				case <-timer.C():
				case <-time.After(time.Second):
					t.Fatal("timer did not become ready within a generous bound")
				}
			})
		}
	})

	t.Run("a successful pre-expiry stop prevents delivery", func(t *testing.T) {
		for _, tc := range contractCases {
			t.Run(tc.name, func(t *testing.T) {
				clk, advance := tc.new()
				timer := clk.NewTimer(50 * time.Millisecond)

				require.True(t, timer.Stop())
				advance(100 * time.Millisecond)

				select {
				case <-timer.C():
					t.Fatal("a pre-expiry stopped timer must never deliver")
				default:
				}
			})
		}
	})

	t.Run("stop returns false once the timer has fired", func(t *testing.T) {
		for _, tc := range contractCases {
			t.Run(tc.name, func(t *testing.T) {
				clk, advance := tc.new()
				timer := clk.NewTimer(10 * time.Millisecond)
				advance(20 * time.Millisecond)

				select {
				case <-timer.C():
				case <-time.After(time.Second):
					t.Fatal("timer did not fire within a generous bound")
				}

				assert.False(t, timer.Stop())
			})
		}
	})

	t.Run("delivery does not require an already-waiting receiver", func(t *testing.T) {
		for _, tc := range contractCases {
			t.Run(tc.name, func(t *testing.T) {
				clk, advance := tc.new()
				timer := clk.NewTimer(10 * time.Millisecond)

				// Nothing reads timer.C() while advance runs.
				advance(20 * time.Millisecond)

				select {
				case v := <-timer.C():
					assert.False(t, v.IsZero())
				case <-time.After(time.Second):
					t.Fatal("value was not available even after firing without a waiting receiver")
				}
			})
		}
	})
}
