package baseapp

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

// mockClosableStore satisfies storetypes.MultiStore via embedding and adds Close().
type mockClosableStore struct {
	storetypes.MultiStore
	closeCalled bool
	closeErr    error
}

func (m *mockClosableStore) Close() error {
	m.closeCalled = true
	return m.closeErr
}

// mockNonClosableStore satisfies storetypes.MultiStore but has no Close method.
type mockNonClosableStore struct {
	storetypes.MultiStore
}

func TestCloseQueryMultiStore(t *testing.T) {
	t.Run("calls Close on a closable store", func(t *testing.T) {
		ms := &mockClosableStore{}
		closeQueryMultiStore(log.NewNopLogger(), ms)
		require.True(t, ms.closeCalled)
	})

	t.Run("no-op for non-closable store", func(t *testing.T) {
		ms := &mockNonClosableStore{}
		require.NotPanics(t, func() {
			closeQueryMultiStore(log.NewNopLogger(), ms)
		})
	})

	t.Run("logs error when Close fails", func(t *testing.T) {
		var buf bytes.Buffer
		logger := log.NewLogger(&buf)
		ms := &mockClosableStore{closeErr: errors.New("disk full")}
		closeQueryMultiStore(logger, ms)
		require.True(t, ms.closeCalled)
		require.Contains(t, buf.String(), "disk full")
	})
}
