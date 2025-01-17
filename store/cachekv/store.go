package cachekv

import (
	"io"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/store/cachekv/internal"
	"cosmossdk.io/store/internal/btree"
	"cosmossdk.io/store/types"
)

type Store = GStore[[]byte]

var (
	_ types.CacheKVStore = (*Store)(nil)
	_ types.CacheWrap    = (*Store)(nil)
	_ types.BranchStore  = (*Store)(nil)
)

func NewStore(parent types.KVStore) *Store {
	return NewGStore(
		parent,
		func(v []byte) bool { return v == nil },
		func(v []byte) int { return len(v) },
	)
}

// GStore wraps an in-memory cache around an underlying types.KVStore.
type GStore[V any] struct {
	writeSet btree.BTree[V] // always ascending sorted
	parent   types.GKVStore[V]

	// isZero is a function that returns true if the value is considered "zero", for []byte and pointers the zero value
	// is `nil`, zero value is not allowed to set to a key, and it's returned if the key is not found.
	isZero    func(V) bool
	zeroValue V
	// valueLen validates the value before it's set
	valueLen func(V) int
}

// NewStore creates a new Store object
func NewGStore[V any](parent types.GKVStore[V], isZero func(V) bool, valueLen func(V) int) *GStore[V] {
	return &GStore[V]{
		writeSet: btree.NewBTree[V](),
		parent:   parent,
		isZero:   isZero,
		valueLen: valueLen,
	}
}

// GetStoreType implements Store.
func (s *GStore[V]) GetStoreType() types.StoreType {
	return s.parent.GetStoreType()
}

// Clone creates a copy-on-write snapshot of the cache store,
// it only performs a shallow copy so is very fast.
func (s *GStore[V]) Clone() types.BranchStore {
	v := *s
	v.writeSet = s.writeSet.Copy()
	return &v
}

// swapCache swap out the internal cache store and leave the current store unusable.
func (s *GStore[V]) swapCache() btree.BTree[V] {
	cache := s.writeSet
	s.writeSet = btree.BTree[V]{}
	return cache
}

// Restore restores the store cache to a given snapshot, leaving the snapshot unusable.
func (s *GStore[V]) Restore(store types.BranchStore) {
	s.writeSet = store.(*GStore[V]).swapCache()
}

// Get implements types.KVStore.
func (s *GStore[V]) Get(key []byte) V {
	types.AssertValidKey(key)

	value, found := s.writeSet.Get(key)
	if !found {
		return s.parent.Get(key)
	}
	return value
}

// Set implements types.KVStore.
func (s *GStore[V]) Set(key []byte, value V) {
	types.AssertValidKey(key)
	types.AssertValidValueGeneric(value, s.isZero, s.valueLen)

	s.writeSet.Set(key, value)
}

// Has implements types.KVStore.
func (s *GStore[V]) Has(key []byte) bool {
	types.AssertValidKey(key)

	value, found := s.writeSet.Get(key)
	if !found {
		return s.parent.Has(key)
	}
	return !s.isZero(value)
}

// Delete implements types.KVStore.
func (s *GStore[V]) Delete(key []byte) {
	types.AssertValidKey(key)
	s.writeSet.Set(key, s.zeroValue)
}

// Implements Cachetypes.KVStore.
func (s *GStore[V]) Write() {
	s.writeSet.Scan(func(key []byte, value V) bool {
		if s.isZero(value) {
			s.parent.Delete(key)
		} else {
			s.parent.Set(key, value)
		}
		return true
	})
	s.writeSet.Clear()
}

func (s *GStore[V]) Discard() {
	s.writeSet.Clear()
}

// CacheWrap implements CacheWrapper.
func (s *GStore[V]) CacheWrap() types.CacheWrap {
	return NewGStore(s, s.isZero, s.valueLen)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (s *GStore[V]) CacheWrapWithTrace(w io.Writer, tc types.TraceContext) types.CacheWrap {
	panic("cannot CacheWrapWithTrace a cachekv Store")
}

//----------------------------------------
// Iteration

// Iterator implements types.KVStore.
func (s *GStore[V]) Iterator(start, end []byte) corestore.GIterator[V] {
	return s.iterator(start, end, true)
}

// ReverseIterator implements types.KVStore.
func (s *GStore[V]) ReverseIterator(start, end []byte) corestore.GIterator[V] {
	return s.iterator(start, end, false)
}

func (s *GStore[V]) iterator(start, end []byte, ascending bool) corestore.GIterator[V] {
	isoSortedCache := s.writeSet.Copy()

	var (
		err           error
		parent, cache corestore.GIterator[V]
	)

	if ascending {
		parent = s.parent.Iterator(start, end)
		cache, err = isoSortedCache.Iterator(start, end)
	} else {
		parent = s.parent.ReverseIterator(start, end)
		cache, err = isoSortedCache.ReverseIterator(start, end)
	}
	if err != nil {
		panic(err)
	}

	return internal.NewCacheMergeIterator(parent, cache, ascending, s.isZero)
}
