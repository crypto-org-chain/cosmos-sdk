package cache

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

type slice[T any] interface {
	~[]T
}

type cacheEntry[K comparable, V slice[T], T any] struct {
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

func newCacheEntry[K comparable, V slice[T], T any](max int) *cacheEntry[K, V, T] {
	return &cacheEntry[K, V, T]{max: max}
}

func (e *cacheEntry[K, V, T]) get() map[K]V {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.data == nil {
		return nil
	}

	copied := make(map[K]V, len(e.data))
	for k, v := range e.data {
		copied[k] = append([]T(nil), v...)
	}

	return copied
}

func (e *cacheEntry[K, V, T]) set(data map[K]V) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	totalElements := 0
	for _, v := range data {
		totalElements += len(v)
	}

	if e.max > 0 && totalElements > e.max {
		e.overflowed = true
		return
	}

	copied := make(map[K]V, len(data))
	for k, v := range data {
		if len(v) == 0 {
			continue
		}
		sliceCopy := append([]T(nil), v...)
		copied[k] = sliceCopy
	}

	e.data = copied
	e.overflowed = false
}

func (e *cacheEntry[K, V, T]) setEntry(key K, value V) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.data == nil {
		e.data = make(map[K]V)
	}

	currentTotal := 0
	for _, v := range e.data {
		currentTotal += len(v)
	}

	// Subtract old value length if key exists (replacement)
	if old, exists := e.data[key]; exists {
		currentTotal -= len(old)
	}

	newTotal := currentTotal + len(value)

	if e.max > 0 && newTotal > e.max {
		e.overflowed = true
		return
	}

	sliceCopy := append([]T(nil), value...)
	e.data[key] = sliceCopy
	e.overflowed = false
}

func (e *cacheEntry[K, V, T]) deleteEntry(key K) {
	if e.max < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.data != nil {
		delete(e.data, key)
	}
}

func (e *cacheEntry[K, V, T]) hasOverflowed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.overflowed
}

type ValidatorsCache struct {
	unbondingValidators  *cacheEntry[string, []string, string]
	unbondingDelegations *cacheEntry[string, []types.DVPair, types.DVPair]
	redelegations        *cacheEntry[string, []types.DVVTriplet, types.DVVTriplet]
}

func NewCache(max int) *ValidatorsCache {
	return &ValidatorsCache{
		unbondingValidators:  newCacheEntry[string, []string, string](max),
		unbondingDelegations: newCacheEntry[string, []types.DVPair, types.DVPair](max),
		redelegations:        newCacheEntry[string, []types.DVVTriplet, types.DVVTriplet](max),
	}
}

func (c *ValidatorsCache) GetUnbondingValidators() map[string][]string {
	return c.unbondingValidators.get()
}

func (c *ValidatorsCache) SetUnbondingValidators(validators map[string][]string) {
	c.unbondingValidators.set(validators)
}

func (c *ValidatorsCache) SetUnbondingValidatorEntry(key string, addrs []string) {
	c.unbondingValidators.setEntry(key, addrs)
}

func (c *ValidatorsCache) DeleteUnbondingValidatorEntry(key string) {
	c.unbondingValidators.deleteEntry(key)
}

func (c *ValidatorsCache) HasUnbondingValidatorsOverflowed() bool {
	return c.unbondingValidators.hasOverflowed()
}

func (c *ValidatorsCache) GetUnbondingDelegations() map[string][]types.DVPair {
	return c.unbondingDelegations.get()
}

func (c *ValidatorsCache) SetUnbondingDelegations(delegations map[string][]types.DVPair) {
	c.unbondingDelegations.set(delegations)
}

func (c *ValidatorsCache) SetUnbondingDelegationEntry(key string, pairs []types.DVPair) {
	c.unbondingDelegations.setEntry(key, pairs)
}

func (c *ValidatorsCache) DeleteUnbondingDelegationEntry(key string) {
	c.unbondingDelegations.deleteEntry(key)
}

func (c *ValidatorsCache) HasUnbondingDelegationsOverflowed() bool {
	return c.unbondingDelegations.hasOverflowed()
}

func (c *ValidatorsCache) GetRedelegations() map[string][]types.DVVTriplet {
	return c.redelegations.get()
}

func (c *ValidatorsCache) SetRedelegations(reds map[string][]types.DVVTriplet) {
	c.redelegations.set(reds)
}

func (c *ValidatorsCache) SetRedelegationEntry(key string, triplets []types.DVVTriplet) {
	c.redelegations.setEntry(key, triplets)
}

func (c *ValidatorsCache) DeleteRedelegationEntry(key string) {
	c.redelegations.deleteEntry(key)
}

func (c *ValidatorsCache) HasRedelegationsOverflowed() bool {
	return c.redelegations.hasOverflowed()
}
