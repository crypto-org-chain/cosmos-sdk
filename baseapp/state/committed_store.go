package state

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/store/v2/cachemulti"
	"github.com/cosmos/cosmos-sdk/store/v2/transient"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

// committedStore is a branch of the last committed version: versioned stores
// come from an immutable snapshot, and transient and object stores, which a
// versioned multi-store hands out live, are replaced by empty private ones
// because that is their committed content.
type committedStore struct {
	snapshot storetypes.CacheMultiStore

	// Filled lazily, by readers that may run concurrently.
	privateStores sync.Map

	// Set before the snapshot is closed. Best effort for a reader that did not
	// pin the state: its next store lookup panics, but a store it already looked
	// up, directly or through a branch, still reads the closed snapshot.
	released atomic.Bool
}

var _ storetypes.CacheMultiStore = (*committedStore)(nil)

const (
	releasedPanic = "check state store used after it was replaced: pin the state, or read it again after Commit"
	writePanic    = "check state is never written to the stores it branches"
)

// branchCommitted branches the last committed version of cms; release is nil
// when the branch holds nothing to free. While the store has no committed version,
// or when it cannot branch that version, it branches the live stores instead;
// the error says why, and the returned store is usable either way.
//
// Limits set by the store, not here:
//   - Reads are isolated from the block in progress only if the store answers
//     CacheMultiStoreWithVersion(latest) with a snapshot.
//   - A pin keeps the branch open, not unpruned. A reader that hits a pruned node
//     makes the store panic, and RunTx turns that into an error once its context is built.
//   - Memory stores are not versioned and stay live.
func branchCommitted(cms storetypes.CommitMultiStore, logger log.Logger) (storetypes.CacheMultiStore, func(), error) {
	version := cms.LatestVersion()
	if version == 0 {
		return cms.CacheMultiStore(), nil, nil
	}

	snapshot, err := cms.CacheMultiStoreWithVersion(version)
	if err != nil {
		// Not a panic: check state is node-local and the next commit retries.
		logger.Error("check state cannot branch the committed version and reads the live stores until the next commit", "version", version, "err", err)
		return cms.CacheMultiStore(), nil, err
	}

	store := &committedStore{snapshot: snapshot}
	closer, ok := snapshot.(io.Closer)
	if !ok {
		return store, nil, nil
	}

	return store, func() {
		store.released.Store(true)
		if err := closer.Close(); err != nil {
			logger.Error("failed to close the committed store branch", "version", version, "err", err)
		}
	}, nil
}

// GetStore tells the stores that reset on commit apart by key type: a branch
// of a transient store reports the type of its in-memory backend, not its own.
func (s *committedStore) GetStore(key storetypes.StoreKey) storetypes.Store {
	if s.released.Load() {
		panic(releasedPanic)
	}

	var empty func() storetypes.CacheWrap
	switch key.(type) {
	case *storetypes.TransientStoreKey:
		empty = func() storetypes.CacheWrap { return transient.NewStore().CacheWrap() }
	case *storetypes.ObjectStoreKey:
		empty = func() storetypes.CacheWrap { return transient.NewObjStore().CacheWrap() }
	default:
		return s.snapshot.GetStore(key)
	}

	private, ok := s.privateStores.Load(key)
	if !ok {
		// Panics for a key that is not mounted, like any other multi-store.
		s.snapshot.GetStore(key)
		private, _ = s.privateStores.LoadOrStore(key, empty())
	}
	return private.(storetypes.Store)
}

func (s *committedStore) GetKVStore(key storetypes.StoreKey) storetypes.KVStore {
	store, ok := s.GetStore(key).(storetypes.KVStore)
	if !ok {
		panic(fmt.Sprintf("store with key %v is not KVStore", key))
	}
	return store
}

func (s *committedStore) GetObjKVStore(key storetypes.StoreKey) storetypes.ObjKVStore {
	store, ok := s.GetStore(key).(storetypes.ObjKVStore)
	if !ok {
		panic(fmt.Sprintf("store with key %v is not ObjKVStore", key))
	}
	return store
}

func (s *committedStore) CacheMultiStore() storetypes.CacheMultiStore {
	return cachemulti.NewFromParent(func(key storetypes.StoreKey) storetypes.CacheWrapper {
		return s.GetStore(key)
	})
}

func (s *committedStore) CacheWrap() storetypes.CacheWrap {
	return s.CacheMultiStore().(storetypes.CacheWrap)
}

func (s *committedStore) GetStoreType() storetypes.StoreType {
	return s.snapshot.GetStoreType()
}

func (s *committedStore) LatestVersion() int64 {
	return s.snapshot.LatestVersion()
}

func (s *committedStore) CacheMultiStoreWithVersion(version int64) (storetypes.CacheMultiStore, error) {
	return s.snapshot.CacheMultiStoreWithVersion(version)
}

// Write panics rather than drop writes silently: nothing writes check state
// back, and the snapshot under it is immutable.
func (s *committedStore) Write() {
	panic(writePanic)
}
