package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	corestoretypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

type slice[T any] interface {
	~[]T
}

type CacheEntry[K comparable, V slice[T], T any] struct {
	mu   sync.RWMutex
	data map[K]V
	// indicates if the cache requires a reload from the store.
	dirty atomic.Bool
	// indicates if the cache is full.
	full atomic.Bool
	// max defines the maximum number of entries in each cache map
	// to prevent OOM attacks.
	// if the size is 0, the cache is unlimited.
	max uint

	loadFromStore func(ctx context.Context) (map[K]V, error)
}

func NewCacheEntry[K comparable, V slice[T], T any](max uint, loadFromStore func(ctx context.Context) (map[K]V, error)) *CacheEntry[K, V, T] {
	entry := &CacheEntry[K, V, T]{max: max, loadFromStore: loadFromStore}
	entry.dirty.Store(true)
	return entry
}

func (e *CacheEntry[K, V, T]) get() map[K]V {
	e.mu.RLock()
	defer e.mu.RUnlock()

	copied := make(map[K]V, len(e.data))

	if e.data == nil {
		return copied
	}

	for k, v := range e.data {
		sliceCopy := make([]T, len(v))
		copy(sliceCopy, v)
		copied[k] = sliceCopy
	}

	return copied
}

func (e *CacheEntry[K, V, T]) getEntry(key K) V {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.data == nil {
		return make([]T, 0)
	}

	value, exists := e.data[key]
	if !exists {
		return make([]T, 0)
	}

	sliceCopy := make([]T, len(value))
	copy(sliceCopy, value)
	return sliceCopy
}

func (e *CacheEntry[K, V, T]) setEntry(key K, value V) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.full.Load() {
		return
	}

	if e.data == nil {
		e.data = make(map[K]V)
	}

	sliceCopy := make([]T, len(value))
	copy(sliceCopy, value)
	e.data[key] = sliceCopy

	if e.max > 0 && uint(len(e.data)) == e.max {
		e.full.Store(true)
	}
}

func (e *CacheEntry[K, V, T]) deleteEntry(key K) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.data == nil {
		return
	}

	delete(e.data, key)
	if e.max > 0 && uint(len(e.data)) < e.max {
		e.full.Store(false)
	}
}

func (e *CacheEntry[K, V, T]) clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.data = make(map[K]V)
	e.full.Store(false)
}

// StoreCacheEntry is a store-backed cache entry (vs in-memory map)
// This is used for unbonding delegations queue to demonstrate rollback behavior
type StoreCacheEntry struct {
	// Mutex only protects bulk load operations to prevent concurrent reloads
	loadMu sync.Mutex

	// Store access (store itself is thread-safe)
	storeService corestoretypes.MemoryStoreService
	cdc          codec.BinaryCodec
	storePrefix  []byte

	// Cache metadata (atomic flags are thread-safe)
	dirty         atomic.Bool
	full          atomic.Bool
	max           uint
	loadFromStore func(ctx context.Context) (map[string][]types.DVPair, error)
	logger        func(ctx context.Context) log.Logger
	name          string
}

func NewStoreCacheEntry(
	storeService corestoretypes.MemoryStoreService,
	cdc codec.BinaryCodec,
	storePrefix []byte,
	max uint,
	loadFromStore func(ctx context.Context) (map[string][]types.DVPair, error),
	name string,
	logger func(ctx context.Context) log.Logger,
) *StoreCacheEntry {
	entry := &StoreCacheEntry{
		storeService:  storeService,
		cdc:           cdc,
		storePrefix:   storePrefix,
		max:           max,
		loadFromStore: loadFromStore,
		name:          name,
		logger:        logger,
	}
	entry.dirty.Store(true)
	return entry
}

// getEntry retrieves a single entry from the store
func (e *StoreCacheEntry) getEntry(ctx context.Context, key string) ([]types.DVPair, error) {
	if e.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if e.dirty.Load() {
		// For store-backed cache, dirty flag means we should skip cache
		// and read from main store instead
		return nil, fmt.Errorf("cache is dirty")
	}

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := append(e.storePrefix, []byte(key)...)

	bz, err := store.Get(storeKey)
	if err != nil {
		return nil, err
	}

	if bz == nil {
		return []types.DVPair{}, nil
	}

	var pairs types.DVPairs
	if err := e.cdc.Unmarshal(bz, &pairs); err != nil {
		return nil, err
	}

	return pairs.Pairs, nil
}

// setEntry stores a single entry in the store
// No mutex needed - store operations are thread-safe
func (e *StoreCacheEntry) setEntry(ctx context.Context, key string, value []types.DVPair) error {
	if e.full.Load() {
		return types.ErrCacheMaxSizeReached
	}

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := append(e.storePrefix, []byte(key)...)

	pairs := types.DVPairs{Pairs: value}
	bz, err := e.cdc.Marshal(&pairs)
	if err != nil {
		return err
	}

	if err := store.Set(storeKey, bz); err != nil {
		return err
	}

	// Check if we've hit the max size
	if e.max > 0 {
		count, err := e.countEntries(ctx)
		if err != nil {
			return err
		}
		if count >= e.max {
			e.full.Store(true)
		}
	}

	return nil
}

// deleteEntry removes an entry from the store
// No mutex needed - store operations are thread-safe
func (e *StoreCacheEntry) deleteEntry(ctx context.Context, key string) error {
	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := append(e.storePrefix, []byte(key)...)

	if err := store.Delete(storeKey); err != nil {
		return err
	}

	// Check if we're now below max size
	if e.max > 0 {
		count, err := e.countEntries(ctx)
		if err != nil {
			return err
		}
		if count < e.max {
			e.full.Store(false)
		}
	}

	return nil
}

// countEntries counts total entries in the store (for size limit checking)
func (e *StoreCacheEntry) countEntries(ctx context.Context) (uint, error) {
	store := e.storeService.OpenMemoryStore(ctx)

	// Iterate to count
	iter, err := store.Iterator(e.storePrefix, storetypes.PrefixEndBytes(e.storePrefix))
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	count := uint(0)
	for ; iter.Valid(); iter.Next() {
		count++
	}

	return count, nil
}

// clear removes all entries from the store
// No mutex needed - store operations are thread-safe
func (e *StoreCacheEntry) clear(ctx context.Context) error {
	store := e.storeService.OpenMemoryStore(ctx)

	// Iterate and delete all
	iter, err := store.Iterator(e.storePrefix, storetypes.PrefixEndBytes(e.storePrefix))
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		if err := store.Delete(iter.Key()); err != nil {
			return err
		}
	}

	e.full.Store(false)
	return nil
}

// getAll retrieves all entries from the store
// Automatically reloads from main store if cache is dirty
func (e *StoreCacheEntry) getAll(ctx context.Context) (map[string][]types.DVPair, error) {
	if e.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	// If cache is dirty, reload from main store first
	if e.dirty.Load() {
		if err := e.load(ctx); err != nil {
			return nil, err
		}
	}

	store := e.storeService.OpenMemoryStore(ctx)
	result := make(map[string][]types.DVPair)

	// Iterate through all entries with our prefix
	iter, err := store.Iterator(e.storePrefix, storetypes.PrefixEndBytes(e.storePrefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	prefixLen := len(e.storePrefix)
	for ; iter.Valid(); iter.Next() {
		// Remove prefix to get the actual key
		key := string(iter.Key()[prefixLen:])

		var pairs types.DVPairs
		if err := e.cdc.Unmarshal(iter.Value(), &pairs); err != nil {
			return nil, err
		}

		result[key] = pairs.Pairs
	}

	return result, nil
}

// load loads all data from the main store into the cache store
// Mutex protects against concurrent reloads
func (e *StoreCacheEntry) load(ctx context.Context) error {
	e.loadMu.Lock()
	defer e.loadMu.Unlock()

	// Check dirty flag again after acquiring lock (double-check pattern)
	// Another goroutine might have completed the load while we were waiting
	if !e.dirty.Load() {
		return nil
	}

	if e.logger != nil {
		e.logger(ctx).Info(fmt.Sprintf("%s cache is dirty. Reinitializing cache from store.", e.name))
	}

	data, err := e.loadFromStore(ctx)
	if err != nil {
		return err
	}

	// Clear existing data in cache
	if err := e.clear(ctx); err != nil {
		return err
	}

	// Load all entries into the cache store
	for key, value := range data {
		if err := e.setEntry(ctx, key, value); err != nil {
			return err
		}
		if e.full.Load() {
			return types.ErrCacheMaxSizeReached
		}
	}

	e.dirty.Store(false)
	return nil
}

type ValidatorsQueueCache struct {
	unbondingValidatorsQueue  *CacheEntry[string, []string, string]
	unbondingDelegationsQueue *StoreCacheEntry
	redelegationsQueue        *CacheEntry[string, []types.DVVTriplet, types.DVVTriplet]
	logger                    func(ctx context.Context) log.Logger
}

func NewValidatorsQueueCache(
	size uint,
	logger func(ctx context.Context) log.Logger,
	cacheStoreService corestoretypes.MemoryStoreService,
	cdc codec.BinaryCodec,
	loadUnbondingValidators func(ctx context.Context) (map[string][]string, error),
	loadUnbondingDelegations func(ctx context.Context) (map[string][]types.DVPair, error),
	loadRedelegations func(ctx context.Context) (map[string][]types.DVVTriplet, error),
) *ValidatorsQueueCache {
	// Use store-backed cache for unbonding delegations (POC)
	unbondingDelegationsCache := NewStoreCacheEntry(
		cacheStoreService,
		cdc,
		[]byte("ubd_queue/"), // prefix for unbonding delegations in cache store
		size,
		loadUnbondingDelegations, // loader function
		"unbonding_delegations",
		logger, // logger for cache reload notifications
	)

	return NewCache(
		NewCacheEntry(size, loadUnbondingValidators),
		unbondingDelegationsCache,
		NewCacheEntry(size, loadRedelegations),
		logger,
	)
}

func NewCache(
	unbondingValidatorsQueue *CacheEntry[string, []string, string],
	unbondingDelegationsQueue *StoreCacheEntry,
	redelegationsQueue *CacheEntry[string, []types.DVVTriplet, types.DVVTriplet],
	logger func(ctx context.Context) log.Logger,
) *ValidatorsQueueCache {
	return &ValidatorsQueueCache{
		unbondingValidatorsQueue:  unbondingValidatorsQueue,
		unbondingDelegationsQueue: unbondingDelegationsQueue,
		redelegationsQueue:        redelegationsQueue,
		logger:                    logger,
	}
}

func (c *ValidatorsQueueCache) loadUnbondingValidatorsQueue(ctx context.Context) error {
	data, err := c.unbondingValidatorsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	c.unbondingValidatorsQueue.clear()

	for key, value := range data {
		c.unbondingValidatorsQueue.setEntry(key, value)
		if c.unbondingValidatorsQueue.full.Load() {
			return types.ErrCacheMaxSizeReached
		}
	}
	c.unbondingValidatorsQueue.dirty.Store(false)
	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, error) {
	if c.unbondingValidatorsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.unbondingValidatorsQueue.dirty.Load() {
		c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")
		err := c.loadUnbondingValidatorsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.unbondingValidatorsQueue.get(), nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
	if c.unbondingValidatorsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.unbondingValidatorsQueue.dirty.Load() {
		c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")
		err := c.loadUnbondingValidatorsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.unbondingValidatorsQueue.getEntry(types.GetCacheValidatorQueueKey(endTime, endHeight)), nil
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorQueueEntry(ctx context.Context, key string, addrs []string) error {
	if c.unbondingValidatorsQueue.full.Load() {
		c.unbondingValidatorsQueue.dirty.Store(true)
		return types.ErrCacheMaxSizeReached
	}
	c.unbondingValidatorsQueue.setEntry(key, addrs)
	return nil
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorQueueEntry(key string) {
	c.unbondingValidatorsQueue.deleteEntry(key)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context) (map[string][]types.DVPair, error) {
	// getAll handles dirty check and reload automatically
	return c.unbondingDelegationsQueue.getAll(ctx)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVPair, error) {
	// Store-backed cache: read directly from store (through context)
	pairs, err := c.unbondingDelegationsQueue.getEntry(ctx, sdk.FormatTimeString(endTime))
	if err != nil {
		// If cache is dirty or errored, return nil to fallback to main store
		return nil, err
	}
	return pairs, nil
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueueEntry(ctx context.Context, key string, delegations []types.DVPair) error {
	// Store-backed cache: write directly to store (through context)
	return c.unbondingDelegationsQueue.setEntry(ctx, key, delegations)
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationQueueEntry(ctx context.Context, key string) error {
	// Store-backed cache: delete from store (through context)
	return c.unbondingDelegationsQueue.deleteEntry(ctx, key)
}

func (c *ValidatorsQueueCache) loadRedelegationsQueue(ctx context.Context) error {
	data, err := c.redelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	c.redelegationsQueue.clear()

	for key, value := range data {
		c.redelegationsQueue.setEntry(key, value)
		if c.redelegationsQueue.full.Load() {
			return types.ErrCacheMaxSizeReached
		}
	}
	c.redelegationsQueue.dirty.Store(false)
	return nil
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	if c.redelegationsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.redelegationsQueue.dirty.Load() {
		c.logger(ctx).Info("Redelegations queue is dirty. Reinitializing cache from store.")
		err := c.loadRedelegationsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.redelegationsQueue.get(), nil
}

func (c *ValidatorsQueueCache) GetRedelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVVTriplet, error) {
	if c.redelegationsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.redelegationsQueue.dirty.Load() {
		c.logger(ctx).Info("Redelegations queue is dirty. Reinitializing cache from store.")
		err := c.loadRedelegationsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.redelegationsQueue.getEntry(sdk.FormatTimeString(endTime)), nil
}

func (c *ValidatorsQueueCache) SetRedelegationsQueueEntry(ctx context.Context, key string, redelegations []types.DVVTriplet) error {
	if c.redelegationsQueue.full.Load() {
		c.redelegationsQueue.dirty.Store(true)
		return types.ErrCacheMaxSizeReached
	}
	c.redelegationsQueue.setEntry(key, redelegations)
	return nil
}

func (c *ValidatorsQueueCache) DeleteRedelegationsQueueEntry(key string) {
	c.redelegationsQueue.deleteEntry(key)
}
