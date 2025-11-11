package cache

import (
	"context"
	"fmt"
	"sync"
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
	return entry
}

func (e *Entry[V, E]) setEntry(ctx context.Context, cdc codec.BinaryCodec, key string, value V) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setEntryUnsafe(ctx, cdc, key, value)
}

// setEntryUnsafe: caller MUST hold lock
func (e *Entry[V, E]) setEntryUnsafe(ctx context.Context, cdc codec.BinaryCodec, key string, value V) error {
	store := e.storeService.OpenMemoryStore(ctx)

	exist := false
	if e.max > 0 {
		var err error
		exist, err = e.exists(store, key)
		if err != nil {
			return err
		}
	}

	bz, err := marshal(cdc, e.cacheType, value)
	if err != nil {
		return err
	}

	if err := e.set(store, key, bz); err != nil {
		return err
	}

	if e.max > 0 && !exist {
		count, err := e.count(store)
		if err != nil {
			return err
		}

		if count >= uint64(e.max) {
			metadata, err := e.getMetadata(store, cdc)
			if err != nil {
				return err
			}
			metadata.IsDirty = true
			metadata.IsFull = true
			if err := e.setMetadata(store, cdc, metadata); err != nil {
				return err
			}
			return types.ErrCacheMaxSizeReached
		}
	}

	return nil
}

func (e *Entry[V, E]) deleteEntry(ctx context.Context, cdc codec.BinaryCodec, key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	store := e.storeService.OpenMemoryStore(ctx)
	exist := false
	if e.max > 0 {
		var err error
		exist, err = e.exists(store, key)
		if err != nil {
			return err
		}
	}

	if err := e.delete(store, key); err != nil {
		return err
	}

	if e.max > 0 && exist {
		count, err := e.count(store)
		if err != nil {
			return err
		}
		if count < uint64(e.max) {
			metadata, err := e.getMetadata(store, cdc)
			if err != nil {
				return err
			}
			metadata.IsFull = false
			if err := e.setMetadata(store, cdc, metadata); err != nil {
				return err
			}
		}
	}
	return nil
}

// clear: caller MUST hold lock
func (e *Entry[V, E]) clear(ctx context.Context, cdc codec.BinaryCodec) error {
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

	metadata, err := e.getMetadata(store, cdc)
	if err != nil {
		return err
	}
	metadata.IsFull = false
	return e.setMetadata(store, cdc, metadata)
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
	c.unbondingValidatorsQueue.mu.Lock()
	defer c.unbondingValidatorsQueue.mu.Unlock()

	store := c.unbondingValidatorsQueue.storeService.OpenMemoryStore(ctx)

	metadata, err := c.unbondingValidatorsQueue.getMetadata(store, c.cdc)
	if err != nil {
		return err
	}
	if !metadata.IsDirty {
		return nil
	}

	c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")

	data, err := c.unbondingValidatorsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	if err := c.unbondingValidatorsQueue.clear(ctx, c.cdc); err != nil {
		return err
	}

	for key, value := range data {
		if err := c.unbondingValidatorsQueue.setEntryUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}

	metadata.IsDirty = false
	return c.unbondingValidatorsQueue.setMetadata(store, c.cdc, metadata)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, error) {
	full, err := c.unbondingValidatorsQueue.isFull(ctx, c.cdc)
	if err != nil {
		return nil, err
	}

	if full {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingValidatorsQueue(ctx); err != nil {
		return nil, err
	}

	return c.unbondingValidatorsQueue.get(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
	full, err := c.unbondingValidatorsQueue.isFull(ctx, c.cdc)
	if err != nil {
		return nil, err
	}
	if full {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingValidatorsQueue(ctx); err != nil {
		return nil, err
	}

	return c.unbondingValidatorsQueue.getEntry(ctx, c.cdc, types.GetCacheValidatorQueueKey(endTime, endHeight))
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorQueueEntry(ctx context.Context, key string, addrs []string) error {
	return c.unbondingValidatorsQueue.setEntry(ctx, c.cdc, key, addrs)
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorQueueEntry(ctx context.Context, key string) error {
	return c.unbondingValidatorsQueue.deleteEntry(ctx, c.cdc, key)
}

// Unbonding Delegations

func (c *ValidatorsQueueCache) checkReloadUnbondingDelegationsQueue(ctx context.Context) error {
	c.unbondingDelegationsQueue.mu.Lock()
	defer c.unbondingDelegationsQueue.mu.Unlock()

	store := c.unbondingDelegationsQueue.storeService.OpenMemoryStore(ctx)

	metadata, err := c.unbondingDelegationsQueue.getMetadata(store, c.cdc)
	if err != nil {
		return err
	}
	if !metadata.IsDirty {
		return nil
	}

	c.logger(ctx).Info("Unbonding delegations queue is dirty. Reinitializing cache from store.")

	data, err := c.unbondingDelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	if err := c.unbondingDelegationsQueue.clear(ctx, c.cdc); err != nil {
		return err
	}

	for key, value := range data {
		if err := c.unbondingDelegationsQueue.setEntryUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}
	metadata.IsDirty = false
	return c.unbondingDelegationsQueue.setMetadata(store, c.cdc, metadata)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context) (map[string][]types.DVPair, error) {
	full, err := c.unbondingDelegationsQueue.isFull(ctx, c.cdc)
	if err != nil {
		return nil, err
	}
	if full {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadUnbondingDelegationsQueue(ctx); err != nil {
		return nil, err
	}

	return c.unbondingDelegationsQueue.get(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVPair, error) {
	full, err := c.unbondingDelegationsQueue.isFull(ctx, c.cdc)
	if err != nil {
		return nil, err
	}
	if full {
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
	return c.unbondingDelegationsQueue.deleteEntry(ctx, c.cdc, key)
}

// Redelegations Queue

func (c *ValidatorsQueueCache) checkReloadRedelegationsQueue(ctx context.Context) error {
	c.redelegationsQueue.mu.Lock()
	defer c.redelegationsQueue.mu.Unlock()

	store := c.redelegationsQueue.storeService.OpenMemoryStore(ctx)

	metadata, err := c.redelegationsQueue.getMetadata(store, c.cdc)
	if err != nil {
		return err
	}
	if !metadata.IsDirty {
		return nil
	}

	c.logger(ctx).Info("Redelegations queue is dirty. Reinitializing cache from store.")
	data, err := c.redelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	if err := c.redelegationsQueue.clear(ctx, c.cdc); err != nil {
		return err
	}

	for key, value := range data {
		if err := c.redelegationsQueue.setEntryUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}

	metadata.IsDirty = false
	return c.redelegationsQueue.setMetadata(store, c.cdc, metadata)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	full, err := c.redelegationsQueue.isFull(ctx, c.cdc)
	if err != nil {
		return nil, err
	}
	if full {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := c.checkReloadRedelegationsQueue(ctx); err != nil {
		return nil, err
	}

	return c.redelegationsQueue.get(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVVTriplet, error) {
	full, err := c.redelegationsQueue.isFull(ctx, c.cdc)
	if err != nil {
		return nil, err
	}
	if full {
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
	return c.redelegationsQueue.deleteEntry(ctx, c.cdc, key)
}
