package state

import (
	"fmt"
	"io"
	"sync"

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
	resetOnCommit sync.Map
}

var _ storetypes.CacheMultiStore = (*committedStore)(nil)

// branchCommitted branches the last committed version of cms. The returned
// release func is nil when the branch holds no resources. Before the first
// commit there is no version to read, so it falls back to the live stores.
func branchCommitted(cms storetypes.CommitMultiStore, logger log.Logger) (storetypes.CacheMultiStore, func()) {
	version := cms.LatestVersion()
	if version == 0 {
		return cms.CacheMultiStore(), nil
	}

	snapshot, err := cms.CacheMultiStoreWithVersion(version)
	if err != nil {
		logger.Error("failed to branch the committed version, falling back to the live stores", "version", version, "err", err)
		return cms.CacheMultiStore(), nil
	}

	var release func()
	if closer, ok := snapshot.(io.Closer); ok {
		release = func() {
			if err := closer.Close(); err != nil {
				logger.Error("failed to close the committed store branch", "version", version, "err", err)
			}
		}
	}

	return &committedStore{snapshot: snapshot}, release
}

// GetStore tells the stores that reset on commit apart by key type: a branch
// of a transient store reports the type of its in-memory backend, not its own.
func (s *committedStore) GetStore(key storetypes.StoreKey) storetypes.Store {
	var empty func() storetypes.CacheWrap
	switch key.(type) {
	case *storetypes.TransientStoreKey:
		empty = func() storetypes.CacheWrap { return transient.NewStore().CacheWrap() }
	case *storetypes.ObjectStoreKey:
		empty = func() storetypes.CacheWrap { return transient.NewObjStore().CacheWrap() }
	default:
		return s.snapshot.GetStore(key)
	}

	private, ok := s.resetOnCommit.Load(key)
	if !ok {
		// Panics for a key that is not mounted, like any other multi-store.
		s.snapshot.GetStore(key)
		private, _ = s.resetOnCommit.LoadOrStore(key, empty())
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

func (s *committedStore) Write() {
	s.snapshot.Write()
	s.resetOnCommit.Range(func(_, store any) bool {
		store.(storetypes.CacheWrap).Write()
		return true
	})
}
