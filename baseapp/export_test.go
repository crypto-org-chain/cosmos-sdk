package baseapp

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type countingCounter struct {
	noop.Int64Counter
	total atomic.Int64
}

func (c *countingCounter) Add(_ context.Context, incr int64, _ ...metric.AddOption) {
	c.total.Add(incr)
}

// CountCheckStateLiveFallbacks turns telemetry on until the test ends. The instrument is a package
// global, so the caller must not run in parallel with other tests.
func CountCheckStateLiveFallbacks(t *testing.T) (count func() int64) {
	t.Helper()

	prev := inst
	t.Cleanup(func() { inst = prev })

	i := &instrument{}
	require.NoError(t, i.Start(nil))
	counter := &countingCounter{}
	i.CheckStateLiveFallback = counter
	return counter.total.Load
}
