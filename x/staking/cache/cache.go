package cache

import (
	"context"
	"sync"
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

type slice[T any] interface {
	~[]T
}

type cacheEntry[K comparable, V slice[T], T any] struct {
	mu   sync.RWMutex
	data map[K]V
	// indicates if the cache requires a reload from the store
	dirty bool
	// indicates if the cache is full
	full bool
	// max defines the maximum number of entries in each cache map
	// to prevent OOM attacks.
	// - if max == 0, there is no cap on the number of entries in the cache
	// - if max > 0, the cache will cap the number of entries it stores
	// - if max < 0, the cache is a no-op cache.
	max int

	loadFromStore func(ctx context.Context) (map[K]V, error)
}

func NewCacheEntry[K comparable, V slice[T], T any](max int, loadFromStore func(ctx context.Context) (map[K]V, error)) *cacheEntry[K, V, T] {
	return &cacheEntry[K, V, T]{max: max, loadFromStore: loadFromStore, dirty: true}
}

func (e *cacheEntry[K, V, T]) get() map[K]V {
	if e.max < 0 {
		return nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.data == nil {
		return nil
	}

	copied := make(map[K]V, len(e.data))
	for k, v := range e.data {
		sliceCopy := make([]T, len(v))
		copy(sliceCopy, v)
		copied[k] = sliceCopy
	}

	return copied
}

func (e *cacheEntry[K, V, T]) getEntry(key K) V {
	if e.max < 0 {
		return nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.data == nil {
		return nil
	}

	value, exists := e.data[key]
	if !exists {
		return nil
	}

	sliceCopy := make([]T, len(value))
	copy(sliceCopy, value)
	return sliceCopy
}

func (e *cacheEntry[K, V, T]) setEntry(key K, value V) {
	if e.max < 0 {
		return
	}

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

	if len(e.data) == e.max {
		e.full = true
	}
}

func (e *cacheEntry[K, V, T]) deleteEntry(key K) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.data != nil {
		delete(e.data, key)
		if len(e.data) < e.max {
			e.full = false
		}
	}
}

type ValidatorsQueueCache struct {
	unbondingValidatorsQueue  *cacheEntry[string, []string, string]
	unbondingDelegationsQueue *cacheEntry[string, []types.DVPair, types.DVPair]
	redelegationsQueue        *cacheEntry[string, []types.DVVTriplet, types.DVVTriplet]
	logger                    func(ctx context.Context) log.Logger
}

func NewCache(
	unbondingValidatorsQueue *cacheEntry[string, []string, string],
	unbondingDelegationsQueue *cacheEntry[string, []types.DVPair, types.DVPair],
	redelegationsQueue *cacheEntry[string, []types.DVVTriplet, types.DVVTriplet],
	logger func(ctx context.Context) log.Logger,
) *ValidatorsQueueCache {
	return &ValidatorsQueueCache{
		unbondingValidatorsQueue:  unbondingValidatorsQueue,
		unbondingDelegationsQueue: unbondingDelegationsQueue,
		redelegationsQueue:        redelegationsQueue,
		logger:                    logger,
	}
}

func (c *ValidatorsQueueCache) initUnbondingValidatorsQueue(ctx context.Context) error {
	if c.unbondingValidatorsQueue.full {
		c.logger(ctx).Warn("GetUnbondingValidatorsQueue failed. Queue is full. Wait for reinitialization or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingValidatorsQueue.max)
		return types.ErrCacheMaxSizeReached
	}

	if c.unbondingValidatorsQueue.dirty {
		c.logger(ctx).Info("Unbonding validators queue is dirty. Reinitializing cache from store.")
		data, err := c.unbondingValidatorsQueue.loadFromStore(ctx)
		if err != nil {
			return err
		}
		for key, value := range data {
			c.unbondingValidatorsQueue.setEntry(key, value)
			if c.unbondingValidatorsQueue.full {
				c.logger(ctx).Warn("Unbonding validators initialization failed. Queue is full. Wait for subsequent reinitializations or restart the node with a larger cache size for this cache to be valid. max size: %d", c.unbondingValidatorsQueue.max)
				return types.ErrCacheMaxSizeReached
			}
			c.unbondingValidatorsQueue.dirty = false
		}
	}
	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, error) {

	err := c.initUnbondingValidatorsQueue(ctx)
	if err != nil {
		return nil, err
	}

	return c.unbondingValidatorsQueue.get(), nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, error) {
	err := c.initUnbondingValidatorsQueue(ctx)
	if err != nil {
		return nil, err
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

func (c *ValidatorsQueueCache) GetUnbondingDelegationsQueue() (map[string][]types.DVPair, bool) {
	return c.unbondingDelegationsQueue.get()
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationsQueue(delegations map[string][]types.DVPair) {
	c.unbondingDelegationsQueue.set(delegations)
}

func (c *ValidatorsQueueCache) SetUnbondingDelegationQueueEntry(key string, pairs []types.DVPair) {
	c.unbondingDelegationsQueue.setEntry(key, pairs)
}

func (c *ValidatorsQueueCache) DeleteUnbondingDelegationQueueEntry(key string) {
	c.unbondingDelegationsQueue.deleteEntry(key)
}

func (c *ValidatorsQueueCache) GetRedelegationsQueue() (map[string][]types.DVVTriplet, bool) {
	return c.redelegationsQueue.get()
}

func (c *ValidatorsQueueCache) SetRedelegationsQueue(reds map[string][]types.DVVTriplet) {
	c.redelegationsQueue.set(reds)
}

func (c *ValidatorsQueueCache) SetRedelegationEntryQueue(key string, triplets []types.DVVTriplet) {
	c.redelegationsQueue.setEntry(key, triplets)
}

func (c *ValidatorsQueueCache) DeleteRedelegationEntryQueue(key string) {
	c.redelegationsQueue.deleteEntry(key)
}
