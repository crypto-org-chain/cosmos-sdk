package testutil

import (
	"sync"
	"sync/atomic"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

// ClosableBranch is a versioned branch that counts how often it is closed.
type ClosableBranch struct {
	storetypes.MultiStore
	closed  atomic.Int32
	onClose func()
}

func (b *ClosableBranch) Write() {}

func (b *ClosableBranch) Close() error {
	b.closed.Add(1)
	if b.onClose != nil {
		b.onClose()
	}
	return nil
}

func (b *ClosableBranch) Closed() int32 {
	return b.closed.Load()
}

// ClosableCommitMultiStore makes the versioned branches of a real store
// closable, like a store that serves them from snapshots it must release.
type ClosableCommitMultiStore struct {
	storetypes.CommitMultiStore

	// UnclosableBranches hands out the store's own branch, which has nothing to close.
	UnclosableBranches bool
	BranchErr          error
	// OnClose runs inside the Close of every branch opened after it is set.
	OnClose func()

	mu       sync.Mutex
	branches []*ClosableBranch
}

func (c *ClosableCommitMultiStore) CacheMultiStoreWithVersion(version int64) (storetypes.CacheMultiStore, error) {
	if c.BranchErr != nil {
		return nil, c.BranchErr
	}
	snapshot, err := c.CommitMultiStore.CacheMultiStoreWithVersion(version)
	if err != nil || c.UnclosableBranches {
		return snapshot, err
	}
	branch := &ClosableBranch{MultiStore: snapshot, onClose: c.OnClose}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.branches = append(c.branches, branch)
	return branch, nil
}

// ClosedCounts returns how many times each closable branch was closed, oldest first.
func (c *ClosableCommitMultiStore) ClosedCounts() []int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	counts := make([]int32, len(c.branches))
	for i, branch := range c.branches {
		counts[i] = branch.Closed()
	}
	return counts
}
