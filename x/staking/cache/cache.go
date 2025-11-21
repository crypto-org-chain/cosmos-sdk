package cache

import (
	"context"

	corestoretypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
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
	loadUnbondingValidators Loader[[]string],
	loadUnbondingDelegations Loader[[]types.DVPair],
	loadRedelegations Loader[[]types.DVVTriplet],
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

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueAll(ctx context.Context) (map[string][]string, error) {
	return c.unbondingValidatorsQueue.getAll(ctx, c.cdc, c.logger)
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context, key string) ([]string, error) {
	return c.unbondingValidatorsQueue.get(ctx, c.cdc, key, c.logger)
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorsQueue(ctx context.Context, key string, addrs []string) error {
	return c.unbondingValidatorsQueue.set(ctx, c.cdc, key, addrs)
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorsQueue(ctx context.Context, key string) error {
	return c.unbondingValidatorsQueue.delete(ctx, c.cdc, key)
}

// Unbonding Delegations

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueAll(ctx context.Context) (map[string][]types.DVPair, error) {
	return c.unbondingDelegationsQueue.getAll(ctx, c.cdc, c.logger)
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context, key string) ([]types.DVPair, error) {
	return c.unbondingDelegationsQueue.get(ctx, c.cdc, key, c.logger)
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueue(ctx context.Context, key string, delegations []types.DVPair) error {
	return c.unbondingDelegationsQueue.set(ctx, c.cdc, key, delegations)
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationsQueue(ctx context.Context, key string) error {
	return c.unbondingDelegationsQueue.delete(ctx, c.cdc, key)
}

// Redelegations Queue

func (c *ValidatorsQueueCache) GetRedelegationsQueueAll(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	return c.redelegationsQueue.getAll(ctx, c.cdc, c.logger)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context, key string) ([]types.DVVTriplet, error) {
	return c.redelegationsQueue.get(ctx, c.cdc, key, c.logger)
}

func (c *ValidatorsQueueCache) SetRedelegationsQueue(ctx context.Context, key string, redelegations []types.DVVTriplet) error {
	return c.redelegationsQueue.set(ctx, c.cdc, key, redelegations)
}

func (c *ValidatorsQueueCache) DeleteRedelegationsQueue(ctx context.Context, key string) error {
	return c.redelegationsQueue.delete(ctx, c.cdc, key)
}
