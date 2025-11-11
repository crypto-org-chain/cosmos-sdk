package cache

import (
	"context"
	"fmt"

	corestoretypes "cosmossdk.io/core/store"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (e *Entry[V, E]) get(ctx context.Context, cdc codec.BinaryCodec) (map[string]V, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make(map[string]V)

	store := e.storeService.OpenMemoryStore(ctx)
	prefix := e.getPrefix()
	iter, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	prefixLen := len([]byte(e.cacheType))
	for ; iter.Valid(); iter.Next() {
		key := string(iter.Key()[prefixLen:])

		value, err := unmarshal[V](cdc, e.cacheType, iter.Value())
		if err != nil {
			return nil, err
		}

		result[key] = value
	}

	return result, nil
}

func (e *Entry[V, E]) getEntry(ctx context.Context, cdc codec.BinaryCodec, key string) (V, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	store := e.storeService.OpenMemoryStore(ctx)
	storeKey := e.getStoreKey(key)

	bz, err := store.Get(storeKey)
	if err != nil {
		return make(V, 0), err
	}

	if bz == nil {
		return make(V, 0), nil
	}

	return unmarshal[V](cdc, e.cacheType, bz)
}

func (e *Entry[V, E]) isFull(ctx context.Context, cdc codec.BinaryCodec) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	store := e.storeService.OpenMemoryStore(ctx)
	metadata, err := e.getMetadata(store, cdc)
	if err != nil {
		return false, err
	}
	return metadata.IsFull, nil
}

func (e *Entry[V, E]) getStoreKey(key string) []byte {
	prefix := e.getPrefix()
	return append(prefix, []byte(key)...)
}

func (e *Entry[V, E]) getPrefix() []byte {
	return []byte(e.cacheType)
}

func (e *Entry[V, E]) getMetaKey() []byte {
	return []byte(fmt.Sprintf("meta_%s", e.cacheType))
}

// exists: caller MUST hold lock
func (e *Entry[V, E]) exists(store corestoretypes.KVStore, key string) (bool, error) {
	storeKey := e.getStoreKey(key)
	return store.Has(storeKey)
}

// set: caller MUST hold lock
func (e *Entry[V, E]) set(store corestoretypes.KVStore, key string, bz []byte) error {
	storeKey := e.getStoreKey(key)
	return store.Set(storeKey, bz)
}

// delete: caller MUST hold lock
func (e *Entry[V, E]) delete(store corestoretypes.KVStore, key string) error {
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
