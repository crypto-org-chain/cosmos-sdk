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

type EntryType string

const (
	UnbondingValidators  EntryType = "unbonding_validators"
	UnbondingDelegations EntryType = "unbonding_delegations"
	Redelegations        EntryType = "redelegations"
)

type Entry[V ~[]E, E any] struct {
	mu           sync.RWMutex
	storeService corestoretypes.MemoryStoreService

	dirty atomic.Bool
	full  atomic.Bool
	count atomic.Uint64 // track number of entries only if max > 0

	max uint

	loadFromStore func(ctx context.Context) (map[string]V, error)
	cacheType     EntryType
}

func NewEntry[V ~[]E, E any](
	storeService corestoretypes.MemoryStoreService,
	max uint,
	loadFromStore func(ctx context.Context) (map[string]V, error),
	cacheType EntryType,
) *Entry[V, E] {
	if storeService == nil {
		panic(fmt.Sprintf("storeService is nil for cache type %s", cacheType))
	}
	if loadFromStore == nil {
		panic(fmt.Sprintf("loadFromStore is nil for cache type %s", cacheType))
	}
	entry := &Entry[V, E]{
		storeService:  storeService,
		max:           max,
		loadFromStore: loadFromStore,
		cacheType:     cacheType,
	}
	entry.dirty.Store(true)
	return entry
}

func (e *Entry[V, E]) get(ctx context.Context, cdc codec.BinaryCodec) (map[string]V, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	fmt.Printf("[%s] get: full=%v, dirty=%v, count=%d\n",
		e.cacheType, e.full.Load(), e.dirty.Load(), e.count.Load())

	result := make(map[string]V)

	store := e.storeService.OpenMemoryStore(ctx)
	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	prefixLen := len([]byte(e.cacheType))
	for ; iter.Valid(); iter.Next() {
		key := string(iter.Key()[prefixLen:])

		value, err := unmarshal[V](cdc, e.cacheType, iter.Value())
		if err != nil {
			return nil, err
		}

		result[key] = value
	}

	fmt.Printf("[%s] get: returning %d entries\n", e.cacheType, len(result))
	return result, nil
}

func (e *Entry[V, E]) getEntry(ctx context.Context, cdc codec.BinaryCodec, key string) (V, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := e.getStoreKey(key)

	bz, err := store.Get(storeKey)
	if err != nil {
		return make(V, 0), err
	}

	if bz == nil {
		return make(V, 0), nil
	}

	return unmarshal[V](cdc, e.cacheType, bz)
}

func (e *Entry[V, E]) setEntry(ctx context.Context, cdc codec.BinaryCodec, key string, value V) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setEntryUnsafe(ctx, cdc, key, value)
}

// setEntryUnsafe works the same as setEntry but the caller is responsible for holding the lock
func (e *Entry[V, E]) setEntryUnsafe(ctx context.Context, cdc codec.BinaryCodec, key string, value V) error {
	fmt.Printf("[%s] setEntryUnsafe: key=%s, full=%v, dirty=%v, count=%d, max=%d\n",
		e.cacheType, key, e.full.Load(), e.dirty.Load(), e.count.Load(), e.max)

	if e.full.Load() {
		fmt.Printf("[%s] setEntryUnsafe: cache is full, returning error\n", e.cacheType)
		return types.ErrCacheMaxSizeReached
	}

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := e.getStoreKey(key)

	exists := false
	if e.max > 0 {
		existingBz, err := store.Get(storeKey)
		if err != nil {
			return err
		}
		exists = existingBz != nil
	}

	bz, err := marshal(cdc, e.cacheType, value)
	if err != nil {
		return err
	}

	if err := store.Set(storeKey, bz); err != nil {
		return err
	}

	// Only increment counter if this is a new key
	if e.max > 0 && !exists {
		newCount := e.count.Add(1)
		fmt.Printf("[%s] setEntryUnsafe: incremented count to %d (max=%d, exists=%v)\n",
			e.cacheType, newCount, e.max, exists)
		if newCount >= uint64(e.max) {
			e.dirty.Store(true)
			e.full.Store(true)
			fmt.Printf("[%s] setEntryUnsafe: cache now FULL and DIRTY\n", e.cacheType)
			return types.ErrCacheMaxSizeReached
		}
	}

	fmt.Printf("[%s] setEntryUnsafe: successfully set key=%s, count=%d\n", e.cacheType, key, e.count.Load())
	return nil
}

func (e *Entry[V, E]) deleteEntry(ctx context.Context, key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	fmt.Printf("[%s] deleteEntry: key=%s, full=%v, dirty=%v, count=%d, max=%d\n",
		e.cacheType, key, e.full.Load(), e.dirty.Load(), e.count.Load(), e.max)

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := e.getStoreKey(key)

	exists := false
	if e.max > 0 {
		existingBz, err := store.Get(storeKey)
		if err != nil {
			return err
		}
		exists = existingBz != nil
	}

	if err := store.Delete(storeKey); err != nil {
		return err
	}

	// Only decrement counter if the key actually existed
	if e.max > 0 && exists {
		newCount := e.count.Add(^uint64(0)) // Subtract 1 using two's complement
		fmt.Printf("[%s] deleteEntry: decremented count to %d (existed=%v)\n",
			e.cacheType, newCount, exists)
		if newCount < uint64(e.max) {
			e.full.Store(false)
			fmt.Printf("[%s] deleteEntry: cache is no longer FULL (count=%d < max=%d), but dirty=%v\n",
				e.cacheType, newCount, e.max, e.dirty.Load())
		}
	}

	fmt.Printf("[%s] deleteEntry: successfully deleted key=%s, count=%d, full=%v, dirty=%v\n",
		e.cacheType, key, e.count.Load(), e.full.Load(), e.dirty.Load())
	return nil
}

// clearUnsafe clears the cache but the caller is responsible for holding the lock
func (e *Entry[V, E]) clearUnsafe(ctx context.Context) error {
	fmt.Printf("[%s] clearUnsafe: clearing cache, count=%d, full=%v, dirty=%v\n",
		e.cacheType, e.count.Load(), e.full.Load(), e.dirty.Load())

	store := e.storeService.OpenMemoryStore(ctx)
	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return err
	}
	defer iter.Close()

	deletedCount := 0
	for ; iter.Valid(); iter.Next() {
		if err := store.Delete(iter.Key()); err != nil {
			return err
		}
		deletedCount++
	}

	e.count.Store(0)
	e.full.Store(false)
	fmt.Printf("[%s] clearUnsafe: cleared %d entries, count=0, full=false\n",
		e.cacheType, deletedCount)
	return nil
}

func (e *Entry[V, E]) getStoreKey(key string) []byte {
	prefix := e.getPrefix()
	return append(prefix, []byte(key)...)
}

func (e *Entry[V, E]) getPrefix() []byte {
	return []byte(e.cacheType)
}

func marshal[V any](cdc codec.BinaryCodec, cacheType EntryType, value V) ([]byte, error) {
	switch cacheType {
	case UnbondingValidators:
		addrs := any(value).([]string)
		return cdc.Marshal(&types.ValAddresses{Addresses: addrs})
	case UnbondingDelegations:
		pairs := any(value).([]types.DVPair)
		return cdc.Marshal(&types.DVPairs{Pairs: pairs})
	case Redelegations:
		triplets := any(value).([]types.DVVTriplet)
		return cdc.Marshal(&types.DVVTriplets{Triplets: triplets})
	default:
		return nil, fmt.Errorf("unknown cache type: %s", cacheType)
	}
}

func unmarshal[V any](cdc codec.BinaryCodec, cacheType EntryType, bz []byte) (V, error) {
	var zero V
	switch cacheType {
	case UnbondingValidators:
		var valAddrs types.ValAddresses
		if err := cdc.Unmarshal(bz, &valAddrs); err != nil {
			return zero, err
		}
		return any(valAddrs.Addresses).(V), nil
	case UnbondingDelegations:
		var pairs types.DVPairs
		if err := cdc.Unmarshal(bz, &pairs); err != nil {
			return zero, err
		}
		return any(pairs.Pairs).(V), nil
	case Redelegations:
		var triplets types.DVVTriplets
		if err := cdc.Unmarshal(bz, &triplets); err != nil {
			return zero, err
		}
		return any(triplets.Triplets).(V), nil
	default:
		return zero, fmt.Errorf("unknown cache type: %s", cacheType)
	}
}

type ValidatorsQueueCache struct {
	unbondingValidatorsQueue  *Entry[[]string, string]
	unbondingDelegationsQueue *Entry[[]types.DVPair, types.DVPair]
	redelegationsQueue        *Entry[[]types.DVVTriplet, types.DVVTriplet]
	cdc                       codec.BinaryCodec
	logger                    func(ctx context.Context) log.Logger
}

func NewValidatorsQueueCache(
	size uint,
	memStoreService corestoretypes.MemoryStoreService,
	loadUnbondingValidators func(ctx context.Context) (map[string][]string, error),
	loadUnbondingDelegations func(ctx context.Context) (map[string][]types.DVPair, error),
	loadRedelegations func(ctx context.Context) (map[string][]types.DVVTriplet, error),
	cdc codec.BinaryCodec,
	logger func(ctx context.Context) log.Logger,
) *ValidatorsQueueCache {
	return NewCache(
		NewEntry(
			memStoreService,
			size,
			loadUnbondingValidators,
			UnbondingValidators,
		),
		NewEntry(
			memStoreService,
			size,
			loadUnbondingDelegations,
			UnbondingDelegations,
		),
		NewEntry(
			memStoreService,
			size,
			loadRedelegations,
			Redelegations,
		),
		cdc,
		logger,
	)
}

func NewCache(
	unbondingValidatorsQueue *Entry[[]string, string],
	unbondingDelegationsQueue *Entry[[]types.DVPair, types.DVPair],
	redelegationsQueue *Entry[[]types.DVVTriplet, types.DVVTriplet],
	cdc codec.BinaryCodec,
	logger func(ctx context.Context) log.Logger,
) *ValidatorsQueueCache {
	return &ValidatorsQueueCache{
		unbondingValidatorsQueue:  unbondingValidatorsQueue,
		unbondingDelegationsQueue: unbondingDelegationsQueue,
		redelegationsQueue:        redelegationsQueue,
		cdc:                       cdc,
		logger:                    logger,
	}
}

// Unbonding Validators Queue

func (c *ValidatorsQueueCache) checkReloadUnbondingValidatorsQueue(ctx context.Context) error {
	fmt.Printf("[unbonding_validators] checkReload: checking dirty flag=%v\n", c.unbondingValidatorsQueue.dirty.Load())

	if !c.unbondingValidatorsQueue.dirty.Load() {
		fmt.Printf("[unbonding_validators] checkReload: not dirty, skipping reload\n")
		return nil
	}

	c.unbondingValidatorsQueue.mu.Lock()
	defer c.unbondingValidatorsQueue.mu.Unlock()

	// Double-check - another goroutine might have reloaded while we waited
	if !c.unbondingValidatorsQueue.dirty.Load() {
		fmt.Printf("[unbonding_validators] checkReload: no longer dirty after acquiring lock, skipping reload\n")
		return nil
	}

	fmt.Printf("[unbonding_validators] checkReload: STARTING RELOAD from persistent store\n")
	c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")
	data, err := c.unbondingValidatorsQueue.loadFromStore(ctx)
	if err != nil {
		fmt.Printf("[unbonding_validators] checkReload: ERROR loading from store: %v\n", err)
		return err
	}
	fmt.Printf("[unbonding_validators] checkReload: loaded %d entries from persistent store\n", len(data))

	if err := c.unbondingValidatorsQueue.clearUnsafe(ctx); err != nil {
		fmt.Printf("[unbonding_validators] checkReload: ERROR clearing cache: %v\n", err)
		return err
	}
	fmt.Printf("[unbonding_validators] checkReload: cleared cache\n")

	for key, value := range data {
		fmt.Printf("[unbonding_validators] checkReload: setting entry key=%s during reload\n", key)
		if err := c.unbondingValidatorsQueue.setEntryUnsafe(ctx, c.cdc, key, value); err != nil {
			fmt.Printf("[unbonding_validators] checkReload: ERROR setting entry key=%s: %v\n", key, err)
			return err
		}
	}

	c.unbondingValidatorsQueue.dirty.Store(false)
	fmt.Printf("[unbonding_validators] checkReload: RELOAD COMPLETE, dirty=false, count=%d\n",
		c.unbondingValidatorsQueue.count.Load())

	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, error) {
	fmt.Printf("[unbonding_validators] GetUnbondingValidatorsQueue: full=%v, dirty=%v, count=%d\n",
		c.unbondingValidatorsQueue.full.Load(), c.unbondingValidatorsQueue.dirty.Load(),
		c.unbondingValidatorsQueue.count.Load())

	if c.unbondingValidatorsQueue.full.Load() {
		fmt.Printf("[unbonding_validators] GetUnbondingValidatorsQueue: cache is FULL, returning error\n")
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingValidatorsQueue(ctx); err != nil {
		fmt.Printf("[unbonding_validators] GetUnbondingValidatorsQueue: reload failed with error: %v\n", err)
		return nil, err
	}

	return c.unbondingValidatorsQueue.get(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
	if c.unbondingValidatorsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingValidatorsQueue(ctx); err != nil {
		return nil, err
	}

	return c.unbondingValidatorsQueue.getEntry(ctx, c.cdc, types.GetCacheValidatorQueueKey(endTime, endHeight))
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorQueueEntry(ctx context.Context, key string, addrs []string) error {
	fmt.Printf("[unbonding_validators] SetUnbondingValidatorQueueEntry: key=%s\n", key)
	return c.unbondingValidatorsQueue.setEntry(ctx, c.cdc, key, addrs)
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorQueueEntry(ctx context.Context, key string) error {
	fmt.Printf("[unbonding_validators] DeleteUnbondingValidatorQueueEntry: key=%s\n", key)
	return c.unbondingValidatorsQueue.deleteEntry(ctx, key)
}

// Unbonding Delegations

func (c *ValidatorsQueueCache) checkReloadUnbondingDelegationsQueue(ctx context.Context) error {
	if !c.unbondingDelegationsQueue.dirty.Load() {
		return nil
	}

	c.unbondingDelegationsQueue.mu.Lock()
	defer c.unbondingDelegationsQueue.mu.Unlock()

	// Double-check - another goroutine might have reloaded while we waited
	if !c.unbondingDelegationsQueue.dirty.Load() {
		return nil
	}

	c.logger(ctx).Info("Unbonding delegations queue is dirty. Reinitializing cache from store.")

	data, err := c.unbondingDelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	if err := c.unbondingDelegationsQueue.clearUnsafe(ctx); err != nil {
		return err
	}

	for key, value := range data {
		if err := c.unbondingDelegationsQueue.setEntryUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}
	c.unbondingDelegationsQueue.dirty.Store(false)
	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context) (map[string][]types.DVPair, error) {
	if c.unbondingDelegationsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingDelegationsQueue(ctx); err != nil {
		return nil, err
	}

	return c.unbondingDelegationsQueue.get(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVPair, error) {
	if c.unbondingDelegationsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingDelegationsQueue(ctx); err != nil {
		return nil, err
	}

	return c.unbondingDelegationsQueue.getEntry(ctx, c.cdc, sdk.FormatTimeString(endTime))
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueueEntry(ctx context.Context, key string, delegations []types.DVPair) error {
	return c.unbondingDelegationsQueue.setEntry(ctx, c.cdc, key, delegations)
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationQueueEntry(ctx context.Context, key string) error {
	return c.unbondingDelegationsQueue.deleteEntry(ctx, key)
}

// Redelegations Queue

func (c *ValidatorsQueueCache) checkReloadRedelegationsQueue(ctx context.Context) error {
	if !c.redelegationsQueue.dirty.Load() {
		return nil
	}

	c.redelegationsQueue.mu.Lock()
	defer c.redelegationsQueue.mu.Unlock()

	// Double-check - another goroutine might have reloaded while we waited
	if !c.redelegationsQueue.dirty.Load() {
		return nil
	}

	c.logger(ctx).Info("Redelegations queue is dirty. Reinitializing cache from store.")
	data, err := c.redelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	if err := c.redelegationsQueue.clearUnsafe(ctx); err != nil {
		return err
	}

	for key, value := range data {
		if err := c.redelegationsQueue.setEntryUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}

	c.redelegationsQueue.dirty.Store(false)
	return nil
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	if c.redelegationsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadRedelegationsQueue(ctx); err != nil {
		return nil, err
	}

	return c.redelegationsQueue.get(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVVTriplet, error) {
	if c.redelegationsQueue.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadRedelegationsQueue(ctx); err != nil {
		return nil, err
	}

	return c.redelegationsQueue.getEntry(ctx, c.cdc, sdk.FormatTimeString(endTime))
}

func (c *ValidatorsQueueCache) SetRedelegationsQueueEntry(ctx context.Context, key string, redelegations []types.DVVTriplet) error {
	return c.redelegationsQueue.setEntry(ctx, c.cdc, key, redelegations)
}

func (c *ValidatorsQueueCache) DeleteRedelegationsQueueEntry(ctx context.Context, key string) error {
	return c.redelegationsQueue.deleteEntry(ctx, key)
}
