package cache

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

type CacheConfig struct {
	// MaxCacheSize defines the maximum number of entries in each cache map
	// to prevent OOM attacks.
	// - if maxCacheSize == 0, there is no cap on the number of entries in the cache
	// - if maxCacheSize > 0, the cache will cap the number of entries it stores
	// - if maxCacheSize < 0, the cache is a no-op cache.
	MaxCacheSize int
}

type cacheEntry[K comparable, V any] struct {
	mu         sync.RWMutex
	data       map[K]V
	overflowed bool
	config     *CacheConfig
}

func newCacheEntry[K comparable, V any](config *CacheConfig) *cacheEntry[K, V] {
	return &cacheEntry[K, V]{
		config: config,
	}
}

func (e *cacheEntry[K, V]) get() map[K]V {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.data
}

func (e *cacheEntry[K, V]) set(data map[K]V) {
	if e.config.MaxCacheSize < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.config.MaxCacheSize > 0 && len(data) > e.config.MaxCacheSize {
		e.overflowed = true
		return
	}

	e.data = data
	e.overflowed = false
}

func (e *cacheEntry[K, V]) setEntry(key K, value V) {
	if e.config.MaxCacheSize < 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.config.MaxCacheSize > 0 && len(e.data) >= e.config.MaxCacheSize {
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
	if e.config.MaxCacheSize < 0 {
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

func NewCache(config CacheConfig) *Cache {
	return &Cache{
		unbondingValidators:  newCacheEntry[string, []string](&config),
		unbondingDelegations: newCacheEntry[string, []types.DVPair](&config),
		redelegations:        newCacheEntry[string, []types.DVVTriplet](&config),
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
}
