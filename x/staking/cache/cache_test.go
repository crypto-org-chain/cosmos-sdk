package cache_test

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"

	"cosmossdk.io/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/cosmos-sdk/x/staking/cache"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// Global memory store key shared by all tests
// This must be a single instance because the context's multistore tracks stores by key instance
var testMemKey = storetypes.NewMemoryStoreKey(types.CacheStoreKey)

func createTestContext(t *testing.T) context.Context {
	key := storetypes.NewKVStoreKey("test_kv")
	tkey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := testutil.DefaultContextWithMemoryStore(t, key, tkey, testMemKey)
	// Use no-op gas meter for concurrent tests to avoid races
	// Standard gas meters are not thread-safe
	return testCtx.Ctx.WithGasMeter(baseapp.NewNoopGasMeter())
}

func newTestingCache(
	validatorsLoader func(ctx context.Context) (map[string][]string, error),
	delegationsLoader func(ctx context.Context) (map[string][]types.DVPair, error),
	redelegationsLoader func(ctx context.Context) (map[string][]types.DVVTriplet, error),
	cacheSize uint,
) *cache.ValidatorsQueueCache {
	logger := func(ctx context.Context) log.Logger {
		return log.NewNopLogger()
	}

	cacheStoreService := runtime.NewMemStoreService(testMemKey)
	encodingConfig := moduletestutil.MakeTestEncodingConfig(staking.AppModuleBasic{})
	cdc := encodingConfig.Codec

	return cache.NewValidatorsQueueCache(
		cacheSize,
		cacheStoreService,
		validatorsLoader,
		delegationsLoader,
		redelegationsLoader,
		cdc,
		logger,
	)
}

func noOpValidatorsLoader(ctx context.Context) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func noOpDelegationsLoader(ctx context.Context) (map[string][]types.DVPair, error) {
	return map[string][]types.DVPair{}, nil
}

func noOpRedelegationsLoader(ctx context.Context) (map[string][]types.DVVTriplet, error) {
	return map[string][]types.DVVTriplet{}, nil
}

func noOpLoadNewTestingCache(size uint) *cache.ValidatorsQueueCache {
	return newTestingCache(noOpValidatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, size)
}

func clearDirtyFlags(ctx context.Context, cache *cache.ValidatorsQueueCache) []error {
	var errs []error

	if _, err := cache.GetUnbondingValidatorsQueue(ctx); err != nil {
		errs = append(errs, err)
	}
	if _, err := cache.GetUnbondingDelegationsQueue(ctx); err != nil {
		errs = append(errs, err)
	}
	if _, err := cache.GetRedelegationsQueue(ctx); err != nil {
		errs = append(errs, err)
	}

	return errs
}

func TestValidatorsQueueCache_Initialization(t *testing.T) {
	validatorsLoader := func(ctx context.Context) (map[string][]string, error) {
		return map[string][]string{
			"time1": {"val1", "val2"},
			"time2": {"val3"},
		}, nil
	}
	delegationsLoader := func(ctx context.Context) (map[string][]types.DVPair, error) {
		return map[string][]types.DVPair{
			"time1": {{DelegatorAddress: "del1", ValidatorAddress: "val1"}},
		}, nil
	}
	redelegationsLoader := func(ctx context.Context) (map[string][]types.DVVTriplet, error) {
		return map[string][]types.DVVTriplet{
			"time1": {{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"}},
		}, nil
	}

	cache := newTestingCache(validatorsLoader, delegationsLoader, redelegationsLoader, 100)

	require.NotNil(t, cache)
}

func TestValidatorsQueueCache_LoadFromStore(t *testing.T) {
	ctx := createTestContext(t)

	validatorsLoader := func(ctx context.Context) (map[string][]string, error) {
		return map[string][]string{
			"time1": {"val1", "val2"},
			"time2": {"val3"},
		}, nil
	}

	delegationsLoader := func(ctx context.Context) (map[string][]types.DVPair, error) {
		return map[string][]types.DVPair{
			"time1": {{DelegatorAddress: "del1", ValidatorAddress: "val1"}},
		}, nil
	}
	redelegationsLoader := func(ctx context.Context) (map[string][]types.DVVTriplet, error) {
		return map[string][]types.DVVTriplet{
			"time1": {{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"}},
		}, nil
	}

	cache := newTestingCache(validatorsLoader, delegationsLoader, redelegationsLoader, 100)

	// Initially dirty, should load from store
	unbondingValidators, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, unbondingValidators, 2)
	require.Equal(t, []string{"val1", "val2"}, unbondingValidators["time1"])
	require.Equal(t, []string{"val3"}, unbondingValidators["time2"])

	unbondingDelegations, err := cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, unbondingDelegations, 1)
	require.Equal(t, []types.DVPair{{DelegatorAddress: "del1", ValidatorAddress: "val1"}}, unbondingDelegations["time1"])

	redelgations, err := cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, redelgations, 1)
	require.Equal(t, []types.DVVTriplet{{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"}}, redelgations["time1"])
}

func TestValidatorsQueueCache_FullPreventsLoad(t *testing.T) {
	ctx := createTestContext(t)

	validatorsLoader := func(ctx context.Context) (map[string][]string, error) {
		// Return too much data (4 keys > max 3)
		return map[string][]string{
			"time1": {"val1"},
			"time2": {"val2"},
			"time3": {"val3"},
			"time4": {"val4"},
		}, nil
	}

	delegationsLoader := func(ctx context.Context) (map[string][]types.DVPair, error) {
		// Return too much data (4 keys > max 3)
		return map[string][]types.DVPair{
			"time1": {{DelegatorAddress: "del1", ValidatorAddress: "val1"}},
			"time2": {{DelegatorAddress: "del2", ValidatorAddress: "val2"}},
			"time3": {{DelegatorAddress: "del3", ValidatorAddress: "val3"}},
			"time4": {{DelegatorAddress: "del4", ValidatorAddress: "val4"}},
		}, nil
	}

	redelegationsLoader := func(ctx context.Context) (map[string][]types.DVVTriplet, error) {
		// Return too much data (4 keys > max 3)
		return map[string][]types.DVVTriplet{
			"time1": {{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"}},
			"time2": {{DelegatorAddress: "del2", ValidatorSrcAddress: "val2", ValidatorDstAddress: "val3"}},
			"time3": {{DelegatorAddress: "del3", ValidatorSrcAddress: "val3", ValidatorDstAddress: "val4"}},
			"time4": {{DelegatorAddress: "del4", ValidatorSrcAddress: "val4", ValidatorDstAddress: "val5"}},
		}, nil
	}

	cache := newTestingCache(validatorsLoader, delegationsLoader, redelegationsLoader, 3)

	// Try to load unbonding validators - should fail due to exceeding max
	_, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.Error(t, err)
	require.Equal(t, types.ErrCacheMaxSizeReached, err)

	// Try to load unbonding delegations - should fail due to exceeding max
	_, err = cache.GetUnbondingDelegationsQueue(ctx)
	require.Error(t, err)
	require.Equal(t, types.ErrCacheMaxSizeReached, err)

	// Try to load redelegations - should fail due to exceeding max
	_, err = cache.GetRedelegationsQueue(ctx)
	require.Error(t, err)
	require.Equal(t, types.ErrCacheMaxSizeReached, err)
}

func TestValidatorsQueueCache_GetEntry(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(100)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	// Test unbonding validators queue entry
	endTime := time.Now().UTC()
	endHeight := int64(1000)
	valKey := types.GetCacheValidatorQueueKey(endTime, endHeight)

	cache.SetUnbondingValidatorQueueEntry(ctx, valKey, []string{"val1", "val2"})
	valEntry, err := cache.GetUnbondingValidatorsQueueEntry(ctx, endTime, endHeight)
	require.NoError(t, err)
	require.Equal(t, []string{"val1", "val2"}, valEntry)

	// Test unbonding delegations queue entry
	delKey := sdk.FormatTimeString(endTime)
	delPairs := []types.DVPair{
		{DelegatorAddress: "del1", ValidatorAddress: "val1"},
		{DelegatorAddress: "del2", ValidatorAddress: "val2"},
	}

	// clear dirty flags first
	_, err = cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)

	cache.SetUnbondingDelegationsQueueEntry(ctx, delKey, delPairs)
	delEntry, err := cache.GetUnbondingDelegationsQueueEntry(ctx, endTime)
	require.NoError(t, err)
	require.Equal(t, delPairs, delEntry)

	// Test redelegations queue entry
	redKey := sdk.FormatTimeString(endTime)
	redTriplets := []types.DVVTriplet{
		{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"},
		{DelegatorAddress: "del2", ValidatorSrcAddress: "val2", ValidatorDstAddress: "val3"},
	}

	// clear dirty flags first
	_, err = cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)

	cache.SetRedelegationsQueueEntry(ctx, redKey, redTriplets)
	redEntry, err := cache.GetRedelegationsQueueEntry(ctx, endTime)
	require.NoError(t, err)
	require.Equal(t, redTriplets, redEntry)
}

func TestValidatorsQueueCache_SetAndDelete(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(100)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	// Test unbonding validators queue
	err := cache.SetUnbondingValidatorQueueEntry(ctx, "key1", []string{"val1"})
	require.NoError(t, err)
	err = cache.SetUnbondingValidatorQueueEntry(ctx, "key2", []string{"val2"})
	require.NoError(t, err)

	valData, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, valData, 2)

	cache.DeleteUnbondingValidatorQueueEntry(ctx, "key1")
	valData, err = cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, valData, 1)
	require.NotContains(t, valData, "key1")

	// Test unbonding delegations queue
	err = cache.SetUnbondingDelegationsQueueEntry(ctx, "time1", []types.DVPair{
		{DelegatorAddress: "del1", ValidatorAddress: "val1"},
	})
	require.NoError(t, err)
	err = cache.SetUnbondingDelegationsQueueEntry(ctx, "time2", []types.DVPair{
		{DelegatorAddress: "del2", ValidatorAddress: "val2"},
	})
	require.NoError(t, err)

	delData, err := cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, delData, 2)

	cache.DeleteUnbondingDelegationQueueEntry(ctx, "time1")
	delData, err = cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, delData, 1)
	require.NotContains(t, delData, "time1")

	// Test redelegations queue
	err = cache.SetRedelegationsQueueEntry(ctx, "time1", []types.DVVTriplet{
		{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"},
	})
	require.NoError(t, err)
	err = cache.SetRedelegationsQueueEntry(ctx, "time2", []types.DVVTriplet{
		{DelegatorAddress: "del2", ValidatorSrcAddress: "val2", ValidatorDstAddress: "val3"},
	})
	require.NoError(t, err)

	redData, err := cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, redData, 2)

	cache.DeleteRedelegationsQueueEntry(ctx, "time1")
	redData, err = cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, redData, 1)
	require.NotContains(t, redData, "time1")
}

func TestValidatorsQueueCache_FullMarkedDirty(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(2)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	// Test unbonding validators queue
	err := cache.SetUnbondingValidatorQueueEntry(ctx, "key1", []string{"val1"})
	require.NoError(t, err)
	err = cache.SetUnbondingValidatorQueueEntry(ctx, "key2", []string{"val2"})
	require.NoError(t, err)

	// Try to add one more - should mark as full and dirty
	err = cache.SetUnbondingValidatorQueueEntry(ctx, "key3", []string{"val3"})
	require.Error(t, err)
	require.Equal(t, types.ErrCacheMaxSizeReached, err)
	// Test unbonding delegations queue
	err = cache.SetUnbondingDelegationsQueueEntry(ctx, "time1", []types.DVPair{
		{DelegatorAddress: "del1", ValidatorAddress: "val1"},
	})
	require.NoError(t, err)
	err = cache.SetUnbondingDelegationsQueueEntry(ctx, "time2", []types.DVPair{
		{DelegatorAddress: "del2", ValidatorAddress: "val2"},
	})
	require.NoError(t, err)

	// Try to add one more - should mark as full and dirty
	err = cache.SetUnbondingDelegationsQueueEntry(ctx, "time3", []types.DVPair{
		{DelegatorAddress: "del3", ValidatorAddress: "val3"},
	})
	require.Error(t, err)
	require.Equal(t, types.ErrCacheMaxSizeReached, err)

	// Test redelegations queue
	err = cache.SetRedelegationsQueueEntry(ctx, "time1", []types.DVVTriplet{
		{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"},
	})
	require.NoError(t, err)
	err = cache.SetRedelegationsQueueEntry(ctx, "time2", []types.DVVTriplet{
		{DelegatorAddress: "del2", ValidatorSrcAddress: "val2", ValidatorDstAddress: "val3"},
	})
	require.NoError(t, err)

	// Try to add one more - should mark as full and dirty
	err = cache.SetRedelegationsQueueEntry(ctx, "time3", []types.DVVTriplet{
		{DelegatorAddress: "del3", ValidatorSrcAddress: "val3", ValidatorDstAddress: "val4"},
	})
	require.Error(t, err)
	require.Equal(t, types.ErrCacheMaxSizeReached, err)
}

func TestValidatorsQueueCache_UnbondingValidators(t *testing.T) {
	ctx := createTestContext(t)

	validatorsLoader := func(ctx context.Context) (map[string][]string, error) {
		return map[string][]string{
			"key1": {"val1", "val2"},
		}, nil
	}

	cache := newTestingCache(validatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, 100)

	// Load from store
	data, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.Len(t, data["key1"], 2)

	// Set individual entry
	err = cache.SetUnbondingValidatorQueueEntry(ctx, "key2", []string{"val3", "val4"})
	require.NoError(t, err)

	data, err = cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 2)

	// Delete entry
	cache.DeleteUnbondingValidatorQueueEntry(ctx, "key1")
	data, err = cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.NotContains(t, data, "key1")
}

func TestValidatorsQueueCache_UnbondingValidatorsEntry(t *testing.T) {
	ctx := createTestContext(t)

	cache := newTestingCache(noOpValidatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, 100)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	endTime := time.Now().UTC()
	endHeight := int64(1000)
	keyStr := types.GetCacheValidatorQueueKey(endTime, endHeight)

	// Set entry
	validators := []string{"val1", "val2", "val3"}
	err := cache.SetUnbondingValidatorQueueEntry(ctx, keyStr, validators)
	require.NoError(t, err)

	// Get specific entry
	entry, err := cache.GetUnbondingValidatorsQueueEntry(ctx, endTime, endHeight)
	require.NoError(t, err)
	require.Equal(t, validators, entry)
}

func TestValidatorsQueueCache_UnbondingDelegations(t *testing.T) {
	ctx := createTestContext(t)

	delegationsLoader := func(ctx context.Context) (map[string][]types.DVPair, error) {
		return map[string][]types.DVPair{
			"time1": {
				{DelegatorAddress: "del1", ValidatorAddress: "val1"},
				{DelegatorAddress: "del2", ValidatorAddress: "val2"},
			},
		}, nil
	}

	cache := newTestingCache(noOpValidatorsLoader, delegationsLoader, noOpRedelegationsLoader, 100)

	// Load from store
	data, err := cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.Len(t, data["time1"], 2)

	// Set individual entry
	err = cache.SetUnbondingDelegationsQueueEntry(ctx, "time2", []types.DVPair{
		{DelegatorAddress: "del3", ValidatorAddress: "val3"},
	})
	require.NoError(t, err)

	data, err = cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 2)

	// Delete entry
	cache.DeleteUnbondingDelegationQueueEntry(ctx, "time1")
	data, err = cache.GetUnbondingDelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.NotContains(t, data, "time1")
}

func TestValidatorsQueueCache_UnbondingDelegationsEntry(t *testing.T) {
	ctx := createTestContext(t)

	cache := newTestingCache(noOpValidatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, 100)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	endTime := time.Now().UTC()
	keyStr := sdk.FormatTimeString(endTime)

	// Set entry
	pairs := []types.DVPair{
		{DelegatorAddress: "del1", ValidatorAddress: "val1"},
	}
	err := cache.SetUnbondingDelegationsQueueEntry(ctx, keyStr, pairs)
	require.NoError(t, err)

	// Get specific entry
	entry, err := cache.GetUnbondingDelegationsQueueEntry(ctx, endTime)
	require.NoError(t, err)
	require.Equal(t, pairs, entry)
}

func TestValidatorsQueueCache_Redelegations(t *testing.T) {
	ctx := createTestContext(t)

	redelegationsLoader := func(ctx context.Context) (map[string][]types.DVVTriplet, error) {
		return map[string][]types.DVVTriplet{
			"time1": {
				{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"},
			},
		}, nil
	}

	cache := newTestingCache(noOpValidatorsLoader, noOpDelegationsLoader, redelegationsLoader, 100)

	// Load from store
	data, err := cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 1)

	// Set individual entry
	err = cache.SetRedelegationsQueueEntry(ctx, "time2", []types.DVVTriplet{
		{DelegatorAddress: "del2", ValidatorSrcAddress: "val2", ValidatorDstAddress: "val3"},
	})
	require.NoError(t, err)

	data, err = cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 2)

	// Delete entry
	cache.DeleteRedelegationsQueueEntry(ctx, "time1")
	data, err = cache.GetRedelegationsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.NotContains(t, data, "time1")
}

func TestValidatorsQueueCache_RedelegationsEntry(t *testing.T) {
	ctx := createTestContext(t)

	cache := newTestingCache(noOpValidatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, 100)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	endTime := time.Now().UTC()
	keyStr := sdk.FormatTimeString(endTime)

	// Set entry
	triplets := []types.DVVTriplet{
		{DelegatorAddress: "del1", ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"},
	}
	err := cache.SetRedelegationsQueueEntry(ctx, keyStr, triplets)
	require.NoError(t, err)

	// Get specific entry
	entry, err := cache.GetRedelegationsQueueEntry(ctx, endTime)
	require.NoError(t, err)
	require.Equal(t, triplets, entry)
}

// Concurrent operations tests
func TestCacheEntry_ConcurrentReads(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(1000)

	// Clear dirty flags and pre-populate cache
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	for i := 0; i < 100; i++ {
		err := cache.SetUnbondingValidatorQueueEntry(ctx, fmt.Sprintf("key%d", i), []string{fmt.Sprintf("val%d", i)})
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	numReaders := 50
	readsPerReader := 100

	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				data, err := cache.GetUnbondingValidatorsQueue(ctx)
				require.NoError(t, err)
				require.NotEmpty(t, data)
			}
		}()
	}

	wg.Wait()
}

func TestCacheEntry_ConcurrentWrites(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(10000)

	// Clear dirty flags
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	var wg sync.WaitGroup
	numWriters := 50
	writesPerWriter := 100

	wg.Add(numWriters)
	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("key_%d_%d", id, j)
				cache.SetUnbondingValidatorQueueEntry(ctx, key, []string{fmt.Sprintf("val_%d", id)})
			}
		}(i)
	}

	wg.Wait()

	// Verify data integrity
	data, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	// Each writer creates unique keys, so we expect exactly numWriters * writesPerWriter entries
	expectedKeys := numWriters * writesPerWriter
	require.Equal(t, expectedKeys, len(data), "cache should contain exactly %d keys", expectedKeys)
}

func TestCacheEntry_ConcurrentReadWrite(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(10000)

	// Clear dirty flags and pre-populate cache
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	for i := 0; i < 100; i++ {
		err := cache.SetUnbondingValidatorQueueEntry(ctx, fmt.Sprintf("key%d", i), []string{fmt.Sprintf("val%d", i)})
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	numRoutines := 50
	operationsPerRoutine := 100

	// Readers
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				_, _ = cache.GetUnbondingValidatorsQueue(ctx)
			}
		}()
	}

	// Writers
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				key := fmt.Sprintf("new_key_%d_%d", id, j)
				cache.SetUnbondingValidatorQueueEntry(ctx, key, []string{fmt.Sprintf("val_%d", id)})
			}
		}(i)
	}

	wg.Wait()

	// Should complete without race conditions
	data, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestCacheEntry_ConcurrentSetAndDelete(t *testing.T) {
	ctx := createTestContext(t)

	cache := noOpLoadNewTestingCache(10000)

	// Clear dirty flags
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	var wg sync.WaitGroup
	numRoutines := 30
	operationsPerRoutine := 100

	// Writers
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				key := fmt.Sprintf("key_%d", id%10) // Reuse some keys
				cache.SetUnbondingValidatorQueueEntry(ctx, key, []string{fmt.Sprintf("val_%d_%d", id, j)})
			}
		}(i)
	}

	// Deleters
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				key := fmt.Sprintf("key_%d", id%10)
				cache.DeleteUnbondingValidatorQueueEntry(ctx, key)
			}
		}(i)
	}

	wg.Wait()

	// Should complete without panic
	_, _ = cache.GetUnbondingValidatorsQueue(ctx)
}

func TestCacheEntry_LoadMutexPreventsMultipleReloads(t *testing.T) {
	ctx := createTestContext(t)

	// Track how many times the loader is called
	var loadCount atomic.Int32
	validatorsLoader := func(ctx context.Context) (map[string][]string, error) {
		loadCount.Add(1)
		// Simulate some work during loading
		time.Sleep(50 * time.Millisecond)
		return map[string][]string{
			"key1": {"val1", "val2"},
			"key2": {"val3"},
		}, nil
	}

	cache := newTestingCache(validatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, 100)

	// Cache starts dirty, so first access will trigger reload
	// Launch multiple concurrent goroutines that will all try to read when dirty
	var wg sync.WaitGroup
	numRoutines := 20

	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			// All goroutines try to read at the same time
			// This should trigger reload, but loadMu should ensure only one reload happens
			data, err := cache.GetUnbondingValidatorsQueue(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, data)
		}()
	}

	wg.Wait()

	// Verify that loader was called exactly once despite multiple concurrent reads
	// The loadMu should have prevented concurrent reloads
	require.Equal(t, int32(1), loadCount.Load(), "loader should only be called once despite concurrent access")

	// Verify the data is correct
	data, err := cache.GetUnbondingValidatorsQueue(ctx)
	require.NoError(t, err)
	require.Len(t, data, 2)
	require.Equal(t, []string{"val1", "val2"}, data["key1"])
	require.Equal(t, []string{"val3"}, data["key2"])
}

func TestValidatorsQueueCache_ConcurrentOperations(t *testing.T) {
	ctx := createTestContext(t)

	cache := newTestingCache(noOpValidatorsLoader, noOpDelegationsLoader, noOpRedelegationsLoader, 10000)

	// clear dirty flags first
	errs := clearDirtyFlags(ctx, cache)
	require.Len(t, errs, 0)

	var wg sync.WaitGroup
	numRoutines := 30
	operationsPerRoutine := 100

	// Concurrent unbonding validators operations
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				key := fmt.Sprintf("v_%d_%d", id, j)
				cache.SetUnbondingValidatorQueueEntry(ctx, key, []string{fmt.Sprintf("addr_%d", id)})
			}
		}(i)
	}

	// Concurrent unbonding delegations operations
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				key := fmt.Sprintf("d_%d_%d", id, j)
				cache.SetUnbondingDelegationsQueueEntry(ctx, key, []types.DVPair{
					{DelegatorAddress: fmt.Sprintf("del_%d", id), ValidatorAddress: "val"},
				})
			}
		}(i)
	}

	// Concurrent redelegations operations
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				key := fmt.Sprintf("r_%d_%d", id, j)
				cache.SetRedelegationsQueueEntry(ctx, key, []types.DVVTriplet{
					{DelegatorAddress: fmt.Sprintf("del_%d", id), ValidatorSrcAddress: "val1", ValidatorDstAddress: "val2"},
				})
			}
		}(i)
	}

	// Concurrent readers
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerRoutine; j++ {
				_, _ = cache.GetUnbondingValidatorsQueue(ctx)
				_, _ = cache.GetUnbondingDelegationsQueue(ctx)
				_, _ = cache.GetRedelegationsQueue(ctx)
			}
		}()
	}

	wg.Wait()

	// Verify all caches have data
	vData, _ := cache.GetUnbondingValidatorsQueue(ctx)
	dData, _ := cache.GetUnbondingDelegationsQueue(ctx)
	rData, _ := cache.GetRedelegationsQueue(ctx)

	require.NotEmpty(t, vData)
	require.NotEmpty(t, dData)
	require.NotEmpty(t, rData)
}
