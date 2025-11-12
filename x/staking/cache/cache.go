package cache

import (
	"context"
	"time"

	corestoretypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

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
		if err := c.unbondingValidatorsQueue.setUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}

	metadata.IsDirty = false
	return c.unbondingValidatorsQueue.setMetadata(store, c.cdc, metadata)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueAll(ctx context.Context) (map[string][]string, error) {
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

	return c.unbondingValidatorsQueue.getAll(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
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

	return c.unbondingValidatorsQueue.get(ctx, c.cdc, types.GetCacheValidatorQueueKey(endTime, endHeight))
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorsQueue(ctx context.Context, key string, addrs []string) error {
	return c.unbondingValidatorsQueue.set(ctx, c.cdc, key, addrs)
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorsQueue(ctx context.Context, key string) error {
	return c.unbondingValidatorsQueue.delete(ctx, c.cdc, key)
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
		if err := c.unbondingDelegationsQueue.setUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}
	metadata.IsDirty = false
	return c.unbondingDelegationsQueue.setMetadata(store, c.cdc, metadata)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueAll(ctx context.Context) (map[string][]types.DVPair, error) {
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

	return c.unbondingDelegationsQueue.getAll(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context, endTime time.Time) ([]types.DVPair, error) {
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

	return c.unbondingDelegationsQueue.get(ctx, c.cdc, sdk.FormatTimeString(endTime))
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueue(ctx context.Context, key string, delegations []types.DVPair) error {
	return c.unbondingDelegationsQueue.set(ctx, c.cdc, key, delegations)
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationsQueue(ctx context.Context, key string) error {
	return c.unbondingDelegationsQueue.delete(ctx, c.cdc, key)
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
		if err := c.redelegationsQueue.setUnsafe(ctx, c.cdc, key, value); err != nil {
			return err
		}
	}

	metadata.IsDirty = false
	return c.redelegationsQueue.setMetadata(store, c.cdc, metadata)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueueAll(ctx context.Context) (map[string][]types.DVVTriplet, error) {
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

	return c.redelegationsQueue.getAll(ctx, c.cdc)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context, endTime time.Time) ([]types.DVVTriplet, error) {
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

	return c.redelegationsQueue.get(ctx, c.cdc, sdk.FormatTimeString(endTime))
}

func (c *ValidatorsQueueCache) SetRedelegationsQueue(ctx context.Context, key string, redelegations []types.DVVTriplet) error {
	return c.redelegationsQueue.set(ctx, c.cdc, key, redelegations)
}

func (c *ValidatorsQueueCache) DeleteRedelegationsQueue(ctx context.Context, key string) error {
	return c.redelegationsQueue.delete(ctx, c.cdc, key)
}
