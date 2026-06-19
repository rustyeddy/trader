package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireAccountLock(t *testing.T) {
	t.Run("acquires lock successfully", func(t *testing.T) {
		_, release, err := acquireAccountLock(uniqueID(t))
		require.NoError(t, err)
		release()
	})

	t.Run("second attempt on same account fails", func(t *testing.T) {
		id := uniqueID(t)
		_, release, err := acquireAccountLock(id)
		require.NoError(t, err)
		defer release()

		_, _, err2 := acquireAccountLock(id)
		require.Error(t, err2)
		assert.Contains(t, err2.Error(), "another bot is already running")
		assert.Contains(t, err2.Error(), id)
	})

	t.Run("lock is reacquirable after release", func(t *testing.T) {
		id := uniqueID(t)
		_, release, err := acquireAccountLock(id)
		require.NoError(t, err)
		release()

		_, release2, err := acquireAccountLock(id)
		require.NoError(t, err)
		defer release2()
	})

	t.Run("different accounts do not conflict", func(t *testing.T) {
		_, release1, err := acquireAccountLock(uniqueID(t) + "-A")
		require.NoError(t, err)
		defer release1()

		_, release2, err := acquireAccountLock(uniqueID(t) + "-B")
		require.NoError(t, err)
		defer release2()
	})
}

// uniqueID returns a filename-safe ID derived from the test name.
func uniqueID(t *testing.T) string {
	t.Helper()
	safe := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return fmt.Sprintf("test-%s", safe)
}
