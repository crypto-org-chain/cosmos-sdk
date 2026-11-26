package state

import (
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type State struct {
	MultiStore storetypes.CacheMultiStore

	mtx sync.RWMutex
	ctx sdk.Context

	span trace.Span

	// release frees what MultiStore holds once pins, the Manager's reference
	// plus in-flight readers, drops to zero. Nil when there is nothing to free.
	release func()
	pins    atomic.Int64
}

func NewState(ctx sdk.Context, ms storetypes.CacheMultiStore) *State {
	st := &State{
		MultiStore: ms,
		ctx:        ctx,
	}
	st.pins.Store(1)
	return st
}

func (st *State) unpin() {
	if st.pins.Add(-1) == 0 && st.release != nil {
		st.release()
	}
}

// unpinRetired must be deferred before the state lock is taken, so it runs
// after the unlock: closing a store must not block readers.
func unpinRetired(retired **State) {
	if *retired != nil {
		(*retired).unpin()
	}
}

// CacheMultiStore calls and returns a CacheMultiStore on the state's underlying
// CacheMultiStore.
func (st *State) CacheMultiStore() storetypes.CacheMultiStore {
	return st.MultiStore.CacheMultiStore()
}

// SetContext updates the state's context to the context provided.
func (st *State) SetContext(ctx sdk.Context) {
	st.mtx.Lock()
	defer st.mtx.Unlock()
	st.ctx = ctx
}

// Context returns the Context of the state.
func (st *State) Context() sdk.Context {
	st.mtx.RLock()
	defer st.mtx.RUnlock()
	return st.ctx
}
