package cache

import (
	"context"
	"fmt"
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

	populateFromStore func(ctx context.Context) (map[K]V, error)
}

func NewCacheEntry[K comparable, V slice[T], T any](max int, populateFromStore func(ctx context.Context) (map[K]V, error)) *cacheEntry[K, V, T] {
	return &cacheEntry[K, V, T]{max: max, populateFromStore: populateFromStore}
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

func (e *cacheEntry[K, V, T]) set(data map[K]V) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.max > 0 && len(data) > e.max {
		e.full = true
		return
	}

	copied := make(map[K]V, len(data))
	for k, v := range data {
		if len(v) == 0 {
			continue
		}
		sliceCopy := make([]T, len(v))
		copy(sliceCopy, v)
		copied[k] = sliceCopy
	}

	e.data = copied
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


	if _, exists := e.data[key]; exists {
		sliceCopy := make([]T, len(value))
		copy(sliceCopy, value)
		e.data[key] = sliceCopy
		return
	}

	if e.max > 0 && len(e.data) == e.max {
		e.full = true
		return
	}

	sliceCopy := make([]T, len(value))
	copy(sliceCopy, value)
	e.data[key] = sliceCopy

}

func (e *cacheEntry[K, V, T]) deleteEntry(key K) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.data != nil {
		delete(e.data, key)
		if e.full {
			e.dirty = true
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

func (c *ValidatorsQueueCache) fullWarningMessage(ctx context.Context, fnName string, max int) {
	c.logger(ctx).Warn(fmt.Sprintf("%s: queue exceeded capacity. Wait for reinitialization or restart the node with a larger cache size for this cache to function. Max size: %d", fnName, max))
}

func (c *ValidatorsQueueCache) initUnbondingValidatorsCache(ctx context.Context) error {
	unbondingValidators, err := c.unbondingValidatorsQueue.populateFromStore(ctx)
	if err != nil {
		return err
	}
	c.unbondingValidatorsQueue.set(unbondingValidators)
	c.unbondingValidatorsQueue.dirty = false
	return nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueue(ctx context.Context) (map[string][]string, bool, error) {
	if c.unbondingValidatorsQueue.dirty {
		c.logger(ctx).Info("unbonding validators queue is dirty. Reinitializing cache from store.")
		err := c.initUnbondingValidatorsCache(ctx)
		if err != nil {
			return nil, false, err
		}
	}
	full := c.unbondingValidatorsQueue.full
	if full {
		c.fullWarningMessage(ctx, "GetUnbondingValidatorsQueue", c.unbondingValidatorsQueue.max)
	}
	return c.unbondingValidatorsQueue.get(), full, nil
}

func (c *ValidatorsQueueCache) GetUnbondingValidatorsQueueEntry(ctx context.Context, endTime time.Time, endHeight int64) ([]string, bool, error) {
	validators, full, err := c.GetUnbondingValidatorsQueue(ctx)
	if err != nil {
		return nil, false, err
	}
	if full {
		return nil, true, nil
	}
	if validators != nil {
		if addrs, ok := validators[types.GetCacheValidatorQueueKey(endTime, endHeight)]; ok {
			return addrs, false, nil
		}
	}
	return nil, false, nil
}

func (c *ValidatorsQueueCache) SetUnbondingValidatorQueueEntry(ctx context.Context, key string, addrs []string) error {
	_, _, err := c.GetUnbondingValidatorsQueue(ctx)
	if err != nil {
		return err
	}
	c.unbondingValidatorsQueue.setEntry(key, addrs)
	full := c.unbondingValidatorsQueue.full
	if full {
		c.logger(ctx).Warn(
			"GetUnbondingValidatorsQueue: unbonding validators queue exceeded capacity. Wait for reinitialization or restart the node with a larger cache size for this cache to function.",
			"max_size", c.unbondingValidatorsQueue.max,
		)
	}
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
