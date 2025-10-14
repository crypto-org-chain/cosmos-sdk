package cache

import (
	"context"
	"sync"
	"time"

	"cosmossdk.io/log"
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
	dirty bool
	// indicates if the cache is full.
	full bool
	// max defines the maximum number of entries in each cache map
	// to prevent OOM attacks.
	// if the size is 0, the cache is unlimited.
	max uint

	loadFromStore func(ctx context.Context) (map[K]V, error)
}

func NewCacheEntry[K comparable, V slice[T], T any](max uint, loadFromStore func(ctx context.Context) (map[K]V, error)) *CacheEntry[K, V, T] {
	return &CacheEntry[K, V, T]{max: max, loadFromStore: loadFromStore, dirty: true}
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

	if e.full {
		return
	}

	if e.data == nil {
		e.data = make(map[K]V)
	}

	sliceCopy := make([]T, len(value))
	copy(sliceCopy, value)
	e.data[key] = sliceCopy

	if e.max > 0 && uint(len(e.data)) == e.max {
		e.full = true
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
		e.full = false
	}
}

func (e *CacheEntry[K, V, T]) clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.data = make(map[K]V)
	e.full = false
}

type ValidatorsQueueCache struct {
	unbondingValidatorsQueue  *CacheEntry[string, []string, string]
	unbondingDelegationsQueue *CacheEntry[string, []types.DVPair, types.DVPair]
	redelegationsQueue        *CacheEntry[string, []types.DVVTriplet, types.DVVTriplet]
	logger                    func(ctx context.Context) log.Logger
}

func NewValidatorsQueueCache(
	size uint,
	logger func(ctx context.Context) log.Logger,
	loadUnbondingValidators func(ctx context.Context) (map[string][]string, error),
	loadUnbondingDelegations func(ctx context.Context) (map[string][]types.DVPair, error),
	loadRedelegations func(ctx context.Context) (map[string][]types.DVVTriplet, error),
) *ValidatorsQueueCache {
	return NewCache(
		NewCacheEntry(size, loadUnbondingValidators),
		NewCacheEntry(size, loadUnbondingDelegations),
		NewCacheEntry(size, loadRedelegations),
		logger,
	)
}

func NewCache(
	unbondingValidatorsQueue *CacheEntry[string, []string, string],
	unbondingDelegationsQueue *CacheEntry[string, []types.DVPair, types.DVPair],
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
		if c.unbondingValidatorsQueue.full {
			c.logger(ctx).Warn("Unbonding validators initialization failed. Queue is full. Wait for subsequent reinitializations or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingValidatorsQueue.max)
			return types.ErrCacheMaxSizeReached
		}
	}
	c.unbondingValidatorsQueue.dirty = false
	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, error) {
	if c.unbondingValidatorsQueue.full {
		c.logger(ctx).Warn("GetUnbondingValidatorsQueue failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingValidatorsQueue.max)
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.unbondingValidatorsQueue.dirty {
		c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")
		err := c.loadUnbondingValidatorsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.unbondingValidatorsQueue.get(), nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
	if c.unbondingValidatorsQueue.full {
		c.logger(ctx).Warn("GetUnbondingValidatorsQueueEntry failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingValidatorsQueue.max)
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.unbondingValidatorsQueue.dirty {
		c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")
		err := c.loadUnbondingValidatorsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.unbondingValidatorsQueue.getEntry(types.GetCacheValidatorQueueKey(endTime, endHeight)), nil
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorQueueEntry(ctx context.Context, key string, addrs []string) error {
	if c.unbondingValidatorsQueue.full {
		c.unbondingValidatorsQueue.dirty = true
		c.logger(ctx).Warn("SetUnbondingValidatorQueueEntry failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingValidatorsQueue.max)
		return types.ErrCacheMaxSizeReached
	}
	c.unbondingValidatorsQueue.setEntry(key, addrs)
	return nil
}

func (c *ValidatorsQueueCache) DeleteUnbondingValidatorQueueEntry(key string) {
	c.unbondingValidatorsQueue.deleteEntry(key)
}

func (c *ValidatorsQueueCache) loadUnbondingDelegationsQueue(ctx context.Context) error {
	data, err := c.unbondingDelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	c.unbondingDelegationsQueue.clear()

	for key, value := range data {
		c.unbondingDelegationsQueue.setEntry(key, value)
		if c.unbondingDelegationsQueue.full {
			c.logger(ctx).Warn("Unbonding delegations initialization failed. Queue is full. Wait for subsequent reinitializations or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingDelegationsQueue.max)
			return types.ErrCacheMaxSizeReached
		}
	}
	c.unbondingDelegationsQueue.dirty = false
	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue(ctx context.Context) (map[string][]types.DVPair, error) {
	if c.unbondingDelegationsQueue.full {
		c.logger(ctx).Warn("GetUnbondingDelegationsQueue failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingDelegationsQueue.max)
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.unbondingDelegationsQueue.dirty {
		c.logger(ctx).Info("Unbonding delegations queue is dirty. Reinitializing cache from store.")
		err := c.loadUnbondingDelegationsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.unbondingDelegationsQueue.get(), nil
}

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVPair, error) {
	if c.unbondingDelegationsQueue.full {
		c.logger(ctx).Warn("GetUnbondingDelegationsQueueEntry failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingDelegationsQueue.max)
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.unbondingDelegationsQueue.dirty {
		c.logger(ctx).Info("Unbonding delegations queue is dirty. Reinitializing cache from store.")
		err := c.loadUnbondingDelegationsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.unbondingDelegationsQueue.getEntry(sdk.FormatTimeString(endTime)), nil
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueueEntry(ctx context.Context, key string, delegations []types.DVPair) error {
	if c.unbondingDelegationsQueue.full {
		c.unbondingDelegationsQueue.dirty = true
		c.logger(ctx).Warn("SetUnbondingDelegationsQueueEntry failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingDelegationsQueue.max)
		return types.ErrCacheMaxSizeReached
	}
	c.unbondingDelegationsQueue.setEntry(key, delegations)
	return nil
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationQueueEntry(key string) {
	c.unbondingDelegationsQueue.deleteEntry(key)
}

func (c *ValidatorsQueueCache) loadRedelegationsQueue(ctx context.Context) error {
	data, err := c.redelegationsQueue.loadFromStore(ctx)
	if err != nil {
		return err
	}

	c.redelegationsQueue.clear()

	for key, value := range data {
		c.redelegationsQueue.setEntry(key, value)
		if c.redelegationsQueue.full {
			c.logger(ctx).Warn("Redelegations initialization failed. Queue is full. Wait for subsequent reinitializations or restart the node with a larger cache size for this cache to be valid. max size: %d", c.redelegationsQueue.max)
			return types.ErrCacheMaxSizeReached
		}
	}
	c.redelegationsQueue.dirty = false
	return nil
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	if c.redelegationsQueue.full {
		c.logger(ctx).Warn("GetRedelegationsQueue failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.redelegationsQueue.max)
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.redelegationsQueue.dirty {
		c.logger(ctx).Info("Redelegations queue is dirty. Reinitializing cache from store.")
		err := c.loadRedelegationsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.redelegationsQueue.get(), nil
}

func (c *ValidatorsQueueCache) GetRedelegationsQueueEntry(ctx context.Context, endTime time.Time) ([]types.DVVTriplet, error) {
	if c.redelegationsQueue.full {
		c.logger(ctx).Warn("GetRedelegationsQueueEntry failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.redelegationsQueue.max)
		return nil, types.ErrCacheMaxSizeReached
	}

	if c.redelegationsQueue.dirty {
		c.logger(ctx).Info("Redelegations queue is dirty. Reinitializing cache from store.")
		err := c.loadRedelegationsQueue(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c.redelegationsQueue.getEntry(sdk.FormatTimeString(endTime)), nil
}

func (c *ValidatorsQueueCache) SetRedelegationsQueueEntry(ctx context.Context, key string, redelegations []types.DVVTriplet) error {
	if c.redelegationsQueue.full {
		c.redelegationsQueue.dirty = true
		c.logger(ctx).Warn("SetRedelegationsQueueEntry failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.redelegationsQueue.max)
		return types.ErrCacheMaxSizeReached
	}
	c.redelegationsQueue.setEntry(key, redelegations)
	return nil
}

func (c *ValidatorsQueueCache) DeleteRedelegationsQueueEntry(key string) {
	c.redelegationsQueue.deleteEntry(key)
}
