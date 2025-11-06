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

type CacheEntryType string

const (
	UnbondingValidators  CacheEntryType = "unbonding_validators"
	UnbondingDelegations CacheEntryType = "unbonding_delegations"
	Redelegations        CacheEntryType = "redelegations"
)

type CacheEntry[V ~[]E, E any] struct {
	// protects bulk load operations to prevent concurrent reloads
	loadMu sync.Mutex

	storeService corestoretypes.MemoryStoreService

	dirty         atomic.Bool
	full          atomic.Bool
	max           uint
	loadFromStore func(ctx context.Context) (map[string]V, error)

	cacheType CacheEntryType
}

func NewCacheEntry[V ~[]E, E any](
	storeService corestoretypes.MemoryStoreService,
	max uint,
	loadFromStore func(ctx context.Context) (map[string]V, error),
	cacheType CacheEntryType,
) *CacheEntry[V, E] {
	entry := &CacheEntry[V, E]{
		storeService:  storeService,
		max:           max,
		loadFromStore: loadFromStore,
		cacheType:     cacheType,
	}
	entry.dirty.Store(true)
	return entry
}

func (e *CacheEntry[V, E]) getEntry(ctx context.Context, cdc codec.BinaryCodec, logger func(ctx context.Context) log.Logger, key string) (V, error) {
	if e.full.Load() {
		return make(V, 0), types.ErrCacheMaxSizeReached
	}

	if e.dirty.Load() {
		if err := e.reload(ctx, cdc, logger); err != nil {
			return make(V, 0), err
		}
	}

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

func (e *CacheEntry[V, E]) setEntry(ctx context.Context, cdc codec.BinaryCodec, key string, value V) error {
	if e.full.Load() {
		e.dirty.Store(true)
		return types.ErrCacheMaxSizeReached
	}

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := e.getStoreKey(key)

	bz, err := marshal(cdc, e.cacheType, value)
	if err != nil {
		return err
	}

	if err := store.Set(storeKey, bz); err != nil {
		return err
	}

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

func (e *CacheEntry[V, E]) deleteEntry(ctx context.Context, key string) error {
	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := e.getStoreKey(key)

	if err := store.Delete(storeKey); err != nil {
		return err
	}

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

func (e *CacheEntry[V, E]) clear(ctx context.Context) error {
	store := e.storeService.OpenMemoryStore(ctx)
	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))

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

func (e *CacheEntry[V, E]) getAll(ctx context.Context, cdc codec.BinaryCodec, logger func(ctx context.Context) log.Logger) (map[string]V, error) {
	if e.full.Load() {
		return nil, types.ErrCacheMaxSizeReached
	}

	if e.dirty.Load() {
		if err := e.reload(ctx, cdc, logger); err != nil {
			return nil, err
		}
	}

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
		key := string(iter.Key()[prefixLen:]) // Remove prefix to get the actual key

		value, err := unmarshal[V](cdc, e.cacheType, iter.Value())
		if err != nil {
			return nil, err
		}

		result[key] = value
	}

	return result, nil
}

func (e *CacheEntry[V, E]) reload(ctx context.Context, cdc codec.BinaryCodec, logger func(ctx context.Context) log.Logger) error {
	e.loadMu.Lock()
	defer e.loadMu.Unlock()

	// Check dirty flag again after acquiring lock (double-check pattern)
	// Another goroutine might have completed the load while we were waiting
	if !e.dirty.Load() {
		return nil
	}

	if logger != nil {
		logger(ctx).Info(fmt.Sprintf("%s cache is dirty. Reinitializing cache from store.", e.cacheType))
	}

	data, err := e.loadFromStore(ctx)
	if err != nil {
		return err
	}

	if err := e.clear(ctx); err != nil {
		return err
	}

	for key, value := range data {
		if err := e.setEntry(ctx, cdc, key, value); err != nil {
			return err
		}
	}

	e.dirty.Store(false)
	return nil
}

func (e *CacheEntry[V, E]) countEntries(ctx context.Context) (uint, error) {
	store := e.storeService.OpenMemoryStore(ctx)
	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
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

func (e *CacheEntry[V, E]) getStoreKey(key string) []byte {
	prefix := e.getPrefix()
	return append(prefix, []byte(key)...)
}

func (e *CacheEntry[V, E]) getPrefix() []byte {
	return []byte(e.cacheType)
}

func marshal[V any](cdc codec.BinaryCodec, cacheType CacheEntryType, value V) ([]byte, error) {
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

func unmarshal[V any](cdc codec.BinaryCodec, cacheType CacheEntryType, bz []byte) (V, error) {
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
	unbondingValidatorsQueue  *CacheEntry[[]string, string]
	unbondingDelegationsQueue *CacheEntry[[]types.DVPair, types.DVPair]
	redelegationsQueue        *CacheEntry[[]types.DVVTriplet, types.DVVTriplet]
	cdc                       codec.BinaryCodec
	logger                    func(ctx context.Context) log.Logger
}

func NewValidatorsQueueCache(
	size uint,
	cacheStoreService corestoretypes.MemoryStoreService,
	loadUnbondingValidators func(ctx context.Context) (map[string][]string, error),
	loadUnbondingDelegations func(ctx context.Context) (map[string][]types.DVPair, error),
	loadRedelegations func(ctx context.Context) (map[string][]types.DVVTriplet, error),
	cdc codec.BinaryCodec,
	logger func(ctx context.Context) log.Logger,
) *ValidatorsQueueCache {
	return NewCache(
		NewCacheEntry(
			cacheStoreService,
			size,
			loadUnbondingValidators,
			UnbondingValidators,
		),
		NewCacheEntry(
			cacheStoreService,
			size,
			loadUnbondingDelegations,
			UnbondingDelegations,
		),
		NewCacheEntry(
			cacheStoreService,
			size,
			loadRedelegations,
			Redelegations,
		),
		cdc,
		logger,
	)
}

func NewCache(
	unbondingValidatorsQueue *CacheEntry[[]string, string],
	unbondingDelegationsQueue *CacheEntry[[]types.DVPair, types.DVPair],
	redelegationsQueue *CacheEntry[[]types.DVVTriplet, types.DVVTriplet],
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

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, error) {
	return c.unbondingValidatorsQueue.getAll(ctx, c.cdc, c.logger)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
	return c.unbondingValidatorsQueue.getEntry(ctx, c.cdc, c.logger, types.GetCacheValidatorQueueKey(endTime, endHeight))
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorQueueEntry(ctx context.Context, key string, addrs []string) error {
	return c.unbondingValidatorsQueue.setEntry(ctx, c.cdc, key, addrs)
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorQueueEntry(ctx context.Context, key string) error {
	return c.unbondingValidatorsQueue.deleteEntry(ctx, key)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context) (map[string][]types.DVPair, error) {
	return c.unbondingDelegationsQueue.getAll(ctx, c.cdc, c.logger)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVPair, error) {
	return c.unbondingDelegationsQueue.getEntry(ctx, c.cdc, c.logger, sdk.FormatTimeString(endTime))
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueueEntry(ctx context.Context, key string, delegations []types.DVPair) error {
	return c.unbondingDelegationsQueue.setEntry(ctx, c.cdc, key, delegations)
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationQueueEntry(ctx context.Context, key string) error {
	return c.unbondingDelegationsQueue.deleteEntry(ctx, key)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	return c.redelegationsQueue.getAll(ctx, c.cdc, c.logger)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVVTriplet, error) {
	return c.redelegationsQueue.getEntry(ctx, c.cdc, c.logger, sdk.FormatTimeString(endTime))
}

func (c *ValidatorsQueueCache) SetRedelegationsQueueEntry(ctx context.Context, key string, redelegations []types.DVVTriplet) error {
	return c.redelegationsQueue.setEntry(ctx, c.cdc, key, redelegations)
}

func (c *ValidatorsQueueCache) DeleteRedelegationsQueueEntry(ctx context.Context, key string) error {
	return c.redelegationsQueue.deleteEntry(ctx, key)
}
