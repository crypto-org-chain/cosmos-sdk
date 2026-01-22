package cachekv

import (
	"io"

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
func (store *GStore[V]) GetStoreType() types.StoreType {
	return store.parent.GetStoreType()
}

// Clone creates a copy-on-write snapshot of the cache store,
// it only performs a shallow copy so is very fast.
func (store *GStore[V]) Clone() types.BranchStore {
	v := *store
	v.writeSet = store.writeSet.Copy()
	return &v
}

// swapCache swap out the internal cache store and leave the current store unusable.
func (store *GStore[V]) swapCache() btree.BTree[V] {
	cache := store.writeSet
	store.writeSet = btree.BTree[V]{}
	return cache
}

// Restore restores the store cache to a given snapshot, leaving the snapshot unusable.
func (store *GStore[V]) Restore(s types.BranchStore) {
	store.writeSet = s.(*GStore[V]).swapCache()
}

// Get implements types.KVStore.
func (store *GStore[V]) Get(key []byte) V {
	types.AssertValidKey(key)

	value, found := store.writeSet.Get(key)
	if !found {
		return store.parent.Get(key)
	}
	return value
}

// Set implements types.KVStore.
func (store *GStore[V]) Set(key []byte, value V) {
	types.AssertValidKey(key)
	types.AssertValidValueGeneric(value, store.isZero, store.valueLen)

	store.writeSet.Set(key, value)
}

// Has implements types.KVStore.
func (store *GStore[V]) Has(key []byte) bool {
	types.AssertValidKey(key)

	value, found := store.writeSet.Get(key)
	if !found {
		return store.parent.Has(key)
	}
	return !store.isZero(value)
}

// Delete implements types.KVStore.
func (store *GStore[V]) Delete(key []byte) {
	types.AssertValidKey(key)
	store.writeSet.Set(key, store.zeroValue)
}

// Implements Cachetypes.KVStore.
func (store *GStore[V]) Write() {
	store.writeSet.Scan(func(key []byte, value V) bool {
		if store.isZero(value) {
			store.parent.Delete(key)
		} else {
			store.parent.Set(key, value)
		}
		return true
	})
	store.writeSet.Clear()
}

func (store *GStore[V]) Discard() {
	store.writeSet.Clear()
}

// CacheWrap implements CacheWrapper.
func (store *GStore[V]) CacheWrap() types.CacheWrap {
	return NewGStore(store, store.isZero, store.valueLen)
}

// CacheWrapWithTrace implements the CacheWrapper interface.
func (store *GStore[V]) CacheWrapWithTrace(w io.Writer, tc types.TraceContext) types.CacheWrap {
	panic("cannot CacheWrapWithTrace a cachekv Store")
}

//----------------------------------------
// Iteration

// Iterator implements types.KVStore.
func (store *GStore[V]) Iterator(start, end []byte) types.GIterator[V] {
	return store.iterator(start, end, true)
}

// ReverseIterator implements types.KVStore.
func (store *GStore[V]) ReverseIterator(start, end []byte) types.GIterator[V] {
	return store.iterator(start, end, false)
}

func (store *GStore[V]) iterator(start, end []byte, ascending bool) types.GIterator[V] {
	isoSortedCache := store.writeSet.Copy()

	var (
		err           error
		parent, cache types.GIterator[V]
	)

	if ascending {
		parent = store.parent.Iterator(start, end)
		cache, err = isoSortedCache.Iterator(start, end)
	} else {
		parent = store.parent.ReverseIterator(start, end)
		cache, err = isoSortedCache.ReverseIterator(start, end)
	}
	if err != nil {
		panic(err)
	}

	return internal.NewCacheMergeIterator(parent, cache, ascending, store.isZero)
}

func findStartIndex(strL []string, startQ string) int {
	// Modified binary search to find the very first element in >=startQ.
	if len(strL) == 0 {
		return -1
	}

	var left, right, mid int
	right = len(strL) - 1
	for left <= right {
		mid = (left + right) >> 1
		midStr := strL[mid]
		if midStr == startQ {
			// Handle condition where there might be multiple values equal to startQ.
			// We are looking for the very first value < midStr, that i+1 will be the first
			// element >= midStr.
			for i := mid - 1; i >= 0; i-- {
				if strL[i] != midStr {
					return i + 1
				}
			}
			return 0
		}
		if midStr < startQ {
			left = mid + 1
		} else { // midStr > startQ
			right = mid - 1
		}
	}
	if left >= 0 && left < len(strL) && strL[left] >= startQ {
		return left
	}
	return -1
}

func findEndIndex(strL []string, endQ string) int {
	if len(strL) == 0 {
		return -1
	}

	// Modified binary search to find the very first element <endQ.
	var left, right, mid int
	right = len(strL) - 1
	for left <= right {
		mid = (left + right) >> 1
		midStr := strL[mid]
		if midStr == endQ {
			// Handle condition where there might be multiple values equal to endQ.
			// We are looking for the very first value < midStr, that i+1 will be the first
			// element >= midStr.
			for i := mid - 1; i >= 0; i-- {
				if strL[i] < midStr {
					return i + 1
				}
			}
			return 0
		}
		if midStr < endQ {
			left = mid + 1
		} else { // midStr > endQ
			right = mid - 1
		}
	}

	// Binary search failed, now let's find a value less than endQ.
	for i := right; i >= 0; i-- {
		if strL[i] < endQ {
			return i
		}
	}

	return -1
}
