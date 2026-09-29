package ports

import (
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewID_IsUUIDv7_UniqueAndOrdered(t *testing.T) {
	ids := make([]string, 5000)
	seen := map[string]bool{}
	for i := range ids {
		ids[i] = NewID()
		assert.Regexp(t, uuidV7, ids[i])
		assert.False(t, seen[ids[i]], "duplicate id")
		seen[ids[i]] = true
	}
	assert.True(t, sort.StringsAreSorted(ids), "ids from one process are strictly increasing")
}

func TestJobStatus_Terminal(t *testing.T) {
	assert.False(t, JobQueued.Terminal())
	assert.False(t, JobProcessing.Terminal())
	for _, s := range []JobStatus{JobDone, JobFailed, JobAborted, JobSpendBlocked, JobPendingApproval, JobNeedsHuman, JobDead} {
		assert.True(t, s.Terminal(), s)
	}
}
