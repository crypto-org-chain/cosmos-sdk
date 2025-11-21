package cache

import (
	"context"
	"fmt"
	"sync"

	corestoretypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

type Type string

const (
	UnbondingValidators  Type = "unbonding_validators"
	UnbondingDelegations Type = "unbonding_delegations"
	Redelegations        Type = "redelegations"
)

type Entry[V ~[]E, E any] struct {
	mu           sync.RWMutex
	storeService corestoretypes.MemoryStoreService

	max uint

	loadFromStore func(ctx context.Context) (map[string]V, error)
	entryType     Type
}

func NewEntry[V ~[]E, E any](
	storeService corestoretypes.MemoryStoreService,
	max uint,
	loadFromStore func(ctx context.Context) (map[string]V, error),
	entryType Type,
) *Entry[V, E] {
	if storeService == nil {
		panic(fmt.Sprintf("storeService is nil for entry type %s", entryType))
	}
	if loadFromStore == nil {
		panic(fmt.Sprintf("loadFromStore is nil for entry type %s", entryType))
	}
	entry := &Entry[V, E]{
		storeService:  storeService,
		max:           max,
		loadFromStore: loadFromStore,
		entryType:     entryType,
	}
	return entry
}

func (e *Entry[V, E]) getAll(ctx context.Context, cdc codec.BinaryCodec, logger func(ctx context.Context) log.Logger) (map[string]V, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	store := e.storeService.OpenMemoryStore(ctx)

	metadata, err := e.getMetadata(store, cdc)
	if err != nil {
		return nil, err
	}

	if metadata.IsFull {
		return nil, types.ErrCacheMaxSizeReached
	}

	if err := e.checkReload(ctx, store, cdc, logger); err != nil {
		return nil, err
	}

	result := make(map[string]V)

	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	prefixLen := len([]byte(e.entryType))
	for ; iter.Valid(); iter.Next() {
		key := string(iter.Key()[prefixLen:])

		value, err := unmarshal[V](cdc, e.entryType, iter.Value())
		if err != nil {
			return nil, err
		}

		result[key] = value
	}

	return result, nil
}

func (e *Entry[V, E]) get(ctx context.Context, cdc codec.BinaryCodec, key string, logger func(ctx context.Context) log.Logger) (V, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	store := e.storeService.OpenMemoryStore(ctx)

	if err := e.checkReload(ctx, store, cdc, logger); err != nil {
		return make(V, 0), err
	}

	storeKey := e.getStoreKey(key)

	bz, err := store.Get(storeKey)
	if err != nil {
		return make(V, 0), err
	}

	if bz == nil {
		return make(V, 0), nil
	}

	return unmarshal[V](cdc, e.entryType, bz)
}

func (e *Entry[V, E]) set(ctx context.Context, cdc codec.BinaryCodec, key string, value V) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	store := e.storeService.OpenMemoryStore(ctx)

	metadata, err := e.getMetadata(store, cdc)
	if err != nil {
		return err
	}

	if metadata.IsFull {
		return types.ErrCacheMaxSizeReached
	}

	exist := false
	if e.max > 0 {
		var err error
		exist, err = e.exists(store, key)
		if err != nil {
			return err
		}
	}

	bz, err := marshal(cdc, e.entryType, value)
	if err != nil {
		return err
	}

	if err := e.setStore(store, key, bz); err != nil {
		return err
	}

	if e.max > 0 && !exist {
		count, err := e.count(store)
		if err != nil {
			return err
		}

		if count >= uint64(e.max) {
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

func (e *Entry[V, E]) delete(ctx context.Context, cdc codec.BinaryCodec, key string) error {
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

	if err := e.deleteStore(store, key); err != nil {
		return err
	}

	if e.max > 0 && exist {
		metadata, err := e.getMetadata(store, cdc)
		if err != nil {
			return err
		}
		metadata.IsFull = false
		if err := e.setMetadata(store, cdc, metadata); err != nil {
			return err
		}
	}
	return nil
}

func (e *Entry[V, E]) getStoreKey(key string) []byte {
	prefix := e.getPrefix()
	return append(prefix, []byte(key)...)
}

func (e *Entry[V, E]) getPrefix() []byte {
	return []byte(e.entryType)
}

func (e *Entry[V, E]) getMetaKey() []byte {
	return []byte(fmt.Sprintf("meta_%s", e.entryType))
}

// exists: caller MUST hold lock
func (e *Entry[V, E]) exists(store corestoretypes.KVStore, key string) (bool, error) {
	storeKey := e.getStoreKey(key)
	return store.Has(storeKey)
}

// set: caller MUST hold lock
func (e *Entry[V, E]) setStore(store corestoretypes.KVStore, key string, bz []byte) error {
	storeKey := e.getStoreKey(key)
	return store.Set(storeKey, bz)
}

// deleteStore: caller MUST hold lock
func (e *Entry[V, E]) deleteStore(store corestoretypes.KVStore, key string) error {
	storeKey := e.getStoreKey(key)
	return store.Delete(storeKey)
}

// count: caller MUST hold lock
func (e *Entry[V, E]) count(store corestoretypes.KVStore) (uint64, error) {
	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	var count uint64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	return count, nil
}

// getMetadata: caller MUST hold lock
func (e *Entry[V, E]) getMetadata(store corestoretypes.KVStore, cdc codec.BinaryCodec) (types.CacheMetadata, error) {
	bz, err := store.Get(e.getMetaKey())
	if err != nil {
		return types.CacheMetadata{}, err
	}
	if bz == nil {
		return types.CacheMetadata{
			IsDirty: true,
			IsFull:  false,
		}, nil
	}

	var metadata types.CacheMetadata
	if err := cdc.Unmarshal(bz, &metadata); err != nil {
		return types.CacheMetadata{}, err
	}
	return metadata, nil
}

// setMetadata: caller MUST hold lock
func (e *Entry[V, E]) setMetadata(store corestoretypes.KVStore, cdc codec.BinaryCodec, metadata types.CacheMetadata) error {
	bz, err := cdc.Marshal(&metadata)
	if err != nil {
		return err
	}
	return store.Set(e.getMetaKey(), bz)
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

// checkReload: caller MUST hold lock
func (e *Entry[V, E]) checkReload(ctx context.Context, store corestoretypes.KVStore, cdc codec.BinaryCodec, logger func(ctx context.Context) log.Logger) error {
	metadata, err := e.getMetadata(store, cdc)
	if err != nil {
		return err
	}

	if !metadata.IsDirty {
		return nil
	}

	if logger != nil {
		logger(ctx).Info(fmt.Sprintf("%s queue is dirty. Reinitializing cache from store.", e.entryType))
	}

	data, err := e.loadFromStore(ctx)
	if err != nil {
		return err
	}

	size := len(data)
	if e.max > 0 && size >= int(e.max) {
		metadata.IsFull = true
		if err := e.setMetadata(store, cdc, metadata); err != nil {
			return err
		}
		return types.ErrCacheMaxSizeReached
	}

	if err := e.clear(ctx, cdc); err != nil {
		return err
	}

	for key, value := range data {
		bz, err := marshal(cdc, e.entryType, value)
		if err != nil {
			return err
		}

		if err := e.setStore(store, key, bz); err != nil {
			return err
		}
	}

	metadata.IsDirty = false
	return e.setMetadata(store, cdc, metadata)
}
