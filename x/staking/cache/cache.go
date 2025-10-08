package cache

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

type cacheEntry[K comparable, V any] struct {
	mu         sync.RWMutex
	data       map[K]V
	overflowed bool
	// max defines the maximum number of entries in each cache map
	// to prevent OOM attacks.
	// - if max == 0, there is no cap on the number of entries in the cache
	// - if max > 0, the cache will cap the number of entries it stores
	// - if max < 0, the cache is a no-op cache.
	max int
}

func newCacheEntry[K comparable, V any](max int) *cacheEntry[K, V] {
	return &cacheEntry[K, V]{
		max: max,
	}
}

func (e *cacheEntry[K, V]) get() map[K]V {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.data
}

func (e *cacheEntry[K, V]) set(data map[K]V) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.max > 0 && len(data) > e.max {
		e.overflowed = true
		return
	}

	e.data = data
	e.overflowed = false
}

func (e *cacheEntry[K, V]) setEntry(key K, value V) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.max > 0 && len(e.data) >= e.max {
		if _, exists := e.data[key]; !exists {
			e.overflowed = true
			return
		}
	}

	if e.data == nil {
		e.data = make(map[K]V)
	}

	e.data[key] = value
}

func (e *cacheEntry[K, V]) deleteEntry(key K) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.data != nil {
		delete(e.data, key)
	}
}

func (e *cacheEntry[K, V]) hasOverflowed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.overflowed
}

type Cache struct {
	unbondingValidators  *cacheEntry[string, []string]
	unbondingDelegations *cacheEntry[string, []types.DVPair]
	redelegations        *cacheEntry[string, []types.DVVTriplet]
}

func NewCache(max int) *Cache {
	return &Cache{
		unbondingValidators:  newCacheEntry[string, []string](max),
		unbondingDelegations: newCacheEntry[string, []types.DVPair](max),
		redelegations:        newCacheEntry[string, []types.DVVTriplet](max),
	}
}

func (c *Cache) GetUnbondingValidators() map[string][]string {
	return c.unbondingValidators.get()
}

func (c *Cache) SetUnbondingValidators(validators map[string][]string) {
	c.unbondingValidators.set(validators)
}

func (c *Cache) SetUnbondingValidatorEntry(key string, addrs []string) {
	c.unbondingValidators.setEntry(key, addrs)
}

func (c *Cache) DeleteUnbondingValidatorEntry(key string) {
	c.unbondingValidators.deleteEntry(key)
}

func (c *Cache) HasUnbondingValidatorsOverflowed() bool {
	return c.unbondingValidators.hasOverflowed()
}

func (c *Cache) GetUnbondingDelegations() map[string][]types.DVPair {
	return c.unbondingDelegations.get()
}

func (c *Cache) SetUnbondingDelegations(delegations map[string][]types.DVPair) {
	c.unbondingDelegations.set(delegations)
}

func (c *Cache) SetUnbondingDelegationEntry(key string, pairs []types.DVPair) {
	c.unbondingDelegations.setEntry(key, pairs)
}

func (c *Cache) DeleteUnbondingDelegationEntry(key string) {
	c.unbondingDelegations.deleteEntry(key)
}

func (c *Cache) HasUnbondingDelegationsOverflowed() bool {
	return c.unbondingDelegations.hasOverflowed()
}

func (c *Cache) GetRedelegations() map[string][]types.DVVTriplet {
	return c.redelegations.get()
}

func (c *Cache) SetRedelegations(reds map[string][]types.DVVTriplet) {
	c.redelegations.set(reds)
}

func (c *Cache) SetRedelegationEntry(key string, triplets []types.DVVTriplet) {
	c.redelegations.setEntry(key, triplets)
}

func (c *Cache) DeleteRedelegationEntry(key string) {
	c.redelegations.deleteEntry(key)
}

func (c *Cache) HasRedelegationsOverflowed() bool {
	return c.redelegations.hasOverflowed()
}
