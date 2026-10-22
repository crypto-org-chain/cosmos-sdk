package keeper_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
	"time"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/core/header"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtestutil "github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func newQueueTestKeeper(tb testing.TB) (sdk.Context, *stakingkeeper.Keeper, *storetypes.KVStoreKey) {
	tb.Helper()
	return newQueueTestKeeperWithDB(tb, dbm.NewMemDB())
}

func newQueueTestKeeperWithDB(tb testing.TB, db dbm.DB) (sdk.Context, *stakingkeeper.Keeper, *storetypes.KVStoreKey) {
	tb.Helper()
	key := storetypes.NewKVStoreKey(stakingtypes.StoreKey)
	cms := store.NewCommitMultiStore(db, log.NewNopLogger())
	cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, db)
	require.NoError(tb, cms.LoadLatestVersion())
	ctx := sdk.NewContext(cms, cmtproto.Header{Time: queueNow, Height: 10}, false, log.NewNopLogger())

	accountKeeper := stakingtestutil.NewMockAccountKeeper(gomock.NewController(tb))
	accountKeeper.EXPECT().GetModuleAddress(gomock.Any()).DoAndReturn(authtypes.NewModuleAddress).AnyTimes()
	accountKeeper.EXPECT().AddressCodec().Return(address.NewBech32Codec("cosmos")).AnyTimes()

	k := stakingkeeper.NewKeeper(
		moduletestutil.MakeTestEncodingConfig().Codec,
		runtime.NewKVStoreService(key),
		accountKeeper,
		nil,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmosvalcons"),
	)
	return ctx, k, key
}

func entryStrings[E any](entries []E) []string {
	var out []string
	for _, e := range entries {
		out = append(out, fmt.Sprint(e))
	}
	return out
}

func dumpQueue(ctx sdk.Context, key *storetypes.KVStoreKey, prefix []byte) string {
	iter := storetypes.KVStorePrefixIterator(ctx.KVStore(key), prefix)
	defer iter.Close()
	var b bytes.Buffer
	for ; iter.Valid(); iter.Next() {
		fmt.Fprintf(&b, "%x=%x;", iter.Key(), iter.Value())
	}
	return b.String()
}

// TestQueueEarliestMatchesUpstreamScan drives the gated keeper and upstream's ungated scans through
// the same random inserts and end-blocks, and checks they mature the same entries and keep the
// same queues, while the gated keeper's stored time never sorts after the queue's first key.
func TestQueueEarliestMatchesUpstreamScan(t *testing.T) {
	odd := []time.Time{
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(20270, 1, 1, 0, 0, 0, 0, time.UTC),
		// Sorts after present-day keys, so it stays queued as an unsortable first entry.
		time.Date(20310, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	for seed := int64(0); seed < 200; seed++ {
		r := rand.New(rand.NewSource(seed))
		gatedCtx, gated, gatedKey := newQueueTestKeeper(t)
		refCtx, ref, refKey := newQueueTestKeeper(t)
		now := queueNow
		var pending []time.Time
		for step := 0; step < 100; step++ {
			gatedCtx = gatedCtx.WithBlockTime(now).WithHeaderInfo(header.Info{Time: now})
			refCtx = refCtx.WithBlockTime(now).WithHeaderInfo(header.Info{Time: now})

			for i := r.Intn(4); i > 0; i-- {
				at := now.Add(time.Duration(r.Intn(7*3600)-2*3600)*time.Second + time.Duration(r.Intn(3)))
				switch r.Intn(15) {
				case 0:
					at = odd[r.Intn(len(odd))]
				case 1:
					at = now
				}
				pending = append(pending, at)
				ubd := stakingtypes.UnbondingDelegation{DelegatorAddress: fmt.Sprint("del", step, i), ValidatorAddress: "val"}
				red := stakingtypes.Redelegation{DelegatorAddress: fmt.Sprint("del", step, i), ValidatorSrcAddress: "src", ValidatorDstAddress: "dst"}
				toUBD := r.Intn(2) == 0
				for _, k := range []struct {
					ctx sdk.Context
					k   *stakingkeeper.Keeper
				}{{gatedCtx, gated}, {refCtx, ref}} {
					if toUBD {
						require.NoError(t, k.k.InsertUBDQueue(k.ctx, ubd, at))
					} else {
						require.NoError(t, k.k.InsertRedelegationQueue(k.ctx, red, at))
					}
				}
			}

			// Disturb only the gated keeper's stored times: an eager recompute, a fork v0.54.x list, a
			// lost value.
			switch r.Intn(25) {
			case 0:
				require.NoError(t, gated.RecomputeQueueEarliest(gatedCtx))
			case 1:
				gatedCtx.KVStore(gatedKey).Set(stakingtypes.UBDQueueEarliestKey, legacyPendingSlots(8))
			case 2:
				gatedCtx.KVStore(gatedKey).Delete(stakingtypes.RedelegationQueueEarliestKey)
			}

			matureUBD, err := gated.DequeueAllMatureUBDQueue(gatedCtx, now)
			require.NoError(t, err)
			refUBD, err := ref.UpstreamDequeueAllMatureUBDQueue(refCtx, now)
			require.NoError(t, err)
			require.Equal(t, entryStrings(refUBD), entryStrings(matureUBD), "seed %d step %d", seed, step)

			matureRed, err := gated.DequeueAllMatureRedelegationQueue(gatedCtx, time.Time{})
			require.NoError(t, err)
			refRed, err := ref.UpstreamDequeueAllMatureRedelegationQueue(refCtx, time.Time{})
			require.NoError(t, err)
			require.Equal(t, entryStrings(refRed), entryStrings(matureRed), "seed %d step %d", seed, step)

			for _, q := range timeQueues {
				require.Equal(t, dumpQueue(refCtx, refKey, q.prefix), dumpQueue(gatedCtx, gatedKey, q.prefix), "seed %d step %d", seed, step)
				requireEarliestBound(t, gatedCtx, gatedKey, q.prefix, q.earliestKey, q.timeKey)
			}
			now = nextBlockTime(r, now, pending)
		}
	}
}

// nextBlockTime advances by up to an hour, or sometimes lands exactly on a pending entry's time.
func nextBlockTime(r *rand.Rand, now time.Time, pending []time.Time) time.Time {
	if r.Intn(4) == 0 {
		for _, at := range pending {
			if at.After(now) && at.Year() < 10000 && at.Sub(now) < 6*time.Hour {
				return at
			}
		}
	}
	return now.Add(time.Duration(r.Intn(3600)) * time.Second)
}

// TestValidatorQueueEarliestMatchesUpstreamScan drives two keepers through the same unbonding
// validators. The reference runs upstream's scan on every end-block; the gated keeper must unbond
// the same validators and keep the same queue.
func TestValidatorQueueEarliestMatchesUpstreamScan(t *testing.T) {
	const seeds, steps = 100, 60
	gasConfig := storetypes.KVGasConfig()
	skipped, releases := 0, 0
	for seed := int64(0); seed < seeds; seed++ {
		r := rand.New(rand.NewSource(seed))
		gatedCtx, gated, gatedKey := newQueueTestKeeper(t)
		refCtx, ref, refKey := newQueueTestKeeper(t)
		now, height := queueNow, int64(10)
		var queued []stakingtypes.Validator
		next := 0
		for step := 0; step < steps; step++ {
			gatedCtx = gatedCtx.WithBlockTime(now).WithBlockHeight(height)
			refCtx = refCtx.WithBlockTime(now).WithBlockHeight(height)

			// Release a hold, as an UnbondingCanComplete callback would.
			if r.Intn(2) == 0 {
				for _, queuedVal := range queued {
					valAddr, err := sdk.ValAddressFromBech32(queuedVal.GetOperator())
					require.NoError(t, err)
					val, err := gated.GetValidator(gatedCtx, valAddr)
					if err != nil || val.UnbondingOnHoldRefCount == 0 {
						continue // already removed, or not held
					}
					val.UnbondingOnHoldRefCount--
					require.NoError(t, gated.SetValidator(gatedCtx, val))
					require.NoError(t, ref.SetValidator(refCtx, val))
					releases++
					break
				}
			}

			for i := r.Intn(3); i > 0 && next < len(PKs); i-- {
				val, err := stakingtypes.NewValidator(sdk.ValAddress(PKs[next].Address()).String(), PKs[next], stakingtypes.Description{})
				require.NoError(t, err)
				next++
				val.Status = stakingtypes.Unbonding
				val.Tokens = math.NewInt(1)
				val.DelegatorShares = math.LegacyOneDec()
				// Mostly future entries, so the gate has scans to skip; some due now or earlier.
				switch r.Intn(12) {
				case 0:
					val.UnbondingTime = now
				case 1:
					val.UnbondingTime = now.Add(-time.Duration(r.Intn(3600)) * time.Second)
				default:
					val.UnbondingTime = now.Add(time.Duration(60+r.Intn(6*3600)) * time.Second)
				}
				val.UnbondingHeight = height - int64(r.Intn(5))
				if r.Intn(10) == 0 {
					val.UnbondingHeight = height + 1 + int64(r.Intn(3))
				}
				if r.Intn(8) == 0 {
					val.UnbondingOnHoldRefCount = 1
				}
				for _, k := range []struct {
					ctx sdk.Context
					k   *stakingkeeper.Keeper
				}{{gatedCtx, gated}, {refCtx, ref}} {
					require.NoError(t, k.k.SetValidator(k.ctx, val))
					require.NoError(t, k.k.InsertUnbondingValidatorQueue(k.ctx, val))
				}
				queued = append(queued, val)
			}
			// A validator that bonds again leaves the queue.
			if len(queued) > 0 && r.Intn(8) == 0 {
				val := queued[r.Intn(len(queued))]
				require.NoError(t, gated.DeleteValidatorQueue(gatedCtx, val))
				require.NoError(t, ref.DeleteValidatorQueue(refCtx, val))
			}
			// Disturb only the gated keeper's stored time: an eager recompute, a fork v0.54.x list, a
			// lost value.
			switch r.Intn(25) {
			case 0:
				require.NoError(t, gated.RecomputeQueueEarliest(gatedCtx))
			case 1:
				gatedCtx.KVStore(gatedKey).Set(stakingtypes.ValidatorQueueEarliestKey, legacyPendingSlots(16))
			case 2:
				gatedCtx.KVStore(gatedKey).Delete(stakingtypes.ValidatorQueueEarliestKey)
			}

			stored := gatedCtx.KVStore(gatedKey).Get(stakingtypes.ValidatorQueueEarliestKey)
			notDue := bytes.Equal(stored, queueEmptyMarker) || (len(stored) > 1 && bytes.Compare(stored, sdk.FormatTimeBytes(now)) > 0)
			meteredCtx := gatedCtx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			require.NoError(t, gated.UnbondAllMatureValidators(meteredCtx))
			require.NoError(t, ref.UpstreamUnbondAllMatureValidators(refCtx))
			if notDue {
				// A skipped end-block reads the stored time and nothing else.
				oneRead := gasConfig.ReadCostFlat + gasConfig.ReadCostPerByte*uint64(len(stakingtypes.ValidatorQueueEarliestKey)+len(stored))
				require.Equal(t, oneRead, meteredCtx.GasMeter().GasConsumed(), "seed %d step %d: scanned although not due", seed, step)
				skipped++
			}

			for _, prefix := range [][]byte{stakingtypes.ValidatorQueueKey, stakingtypes.ValidatorsKey} {
				require.Equal(t, dumpQueue(refCtx, refKey, prefix), dumpQueue(gatedCtx, gatedKey, prefix), "seed %d step %d", seed, step)
			}
			requireEarliestBound(t, gatedCtx, gatedKey, stakingtypes.ValidatorQueueKey, stakingtypes.ValidatorQueueEarliestKey, validatorQueueKeyAt)

			now = now.Add(time.Duration(r.Intn(1800)) * time.Second)
			height++
		}
	}
	// The comparison only covers the gate if it really skips scans and holds really get released.
	t.Logf("gate skipped %d of %d end-blocks; %d holds released", skipped, seeds*steps, releases)
	require.Greater(t, skipped, seeds*steps/4, "gate skipped too few end-blocks")
	require.Positive(t, releases, "no hold was released")
}

// validatorQueueKeyAt is the smallest validator queue key for t, so it sorts at or before every
// entry at t.
func validatorQueueKeyAt(t time.Time) []byte {
	return stakingtypes.GetValidatorQueueKey(t, 0)
}

// requireEarliestBound checks that the queue key for the stored time sorts at or before the queue's
// first key, and that the empty marker is only stored for an empty queue.
func requireEarliestBound(t *testing.T, ctx sdk.Context, key *storetypes.KVStoreKey, prefix, earliestKey []byte, timeKey func(time.Time) []byte) {
	t.Helper()
	store := ctx.KVStore(key)
	stored := store.Get(earliestKey)
	iter := storetypes.KVStorePrefixIterator(store, prefix)
	defer iter.Close()
	switch {
	case bytes.Equal(stored, queueEmptyMarker):
		require.False(t, iter.Valid(), "empty marker stored for a non-empty queue")
	case stored != nil:
		storedTime, err := sdk.ParseTimeBytes(stored)
		if err != nil {
			return // a legacy list the test injected; it reads as unknown
		}
		if iter.Valid() {
			require.LessOrEqual(t, bytes.Compare(timeKey(storedTime), iter.Key()), 0, "stored time sorts after the first queue key")
		}
	}
}

// BenchmarkQueueEndBlock compares the unbonding queue end-block with upstream v0.55.0's scan on
// goleveldb, whose iterators don't read ahead the way MemDB's do, with 50 and 500 future
// timeslices. Each op also creates a CacheContext, which costs the same in both columns.
func BenchmarkQueueEndBlock(b *testing.B) {
	slice := func(n int) []stakingtypes.DVPair {
		return []stakingtypes.DVPair{{DelegatorAddress: fmt.Sprint("del", n), ValidatorAddress: "val"}}
	}
	for _, future := range []int{50, 500} {
		for _, bc := range []struct {
			name  string
			due   int // timeslices maturing this block
			gated bool
		}{
			{"idle/upstream", 0, false},
			{"idle/gated", 0, true},
			{"due-1/upstream", 1, false},
			{"due-1/gated", 1, true},
		} {
			b.Run(fmt.Sprintf("future-%d/%s", future, bc.name), func(b *testing.B) {
				db, err := dbm.NewGoLevelDB("bench", b.TempDir(), nil)
				require.NoError(b, err)
				ctx, k, _ := newQueueTestKeeperWithDB(b, db)
				ctx = ctx.WithBlockTime(queueNow).WithHeaderInfo(header.Info{Time: queueNow})
				require.NoError(b, k.RecomputeQueueEarliest(ctx))
				for i := range future {
					require.NoError(b, k.SetUBDQueueTimeSlice(ctx, queueNow.Add(time.Duration(i+1)*time.Minute), slice(i)))
				}
				for i := range bc.due {
					require.NoError(b, k.SetUBDQueueTimeSlice(ctx, queueNow.Add(-time.Duration(i+1)*time.Second), slice(i)))
				}
				ctx.MultiStore().(storetypes.CommitMultiStore).Commit()
				dequeue := k.UpstreamDequeueAllMatureUBDQueue
				if bc.gated {
					dequeue = k.DequeueAllMatureUBDQueue
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					cacheCtx, _ := ctx.CacheContext()
					if _, err := dequeue(cacheCtx, queueNow); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkValidatorQueueEndBlock compares the validator queue end-block with upstream's scan on
// goleveldb, with 50 future unbonding validators. In the held case one validator is due but on
// hold, so the queue stays due every block and the gate also looks up the first entry.
func BenchmarkValidatorQueueEndBlock(b *testing.B) {
	for _, bc := range []struct {
		name  string
		held  bool
		gated bool
	}{
		{"idle/upstream", false, false},
		{"idle/gated", false, true},
		{"held/upstream", true, false},
		{"held/gated", true, true},
	} {
		b.Run(bc.name, func(b *testing.B) {
			db, err := dbm.NewGoLevelDB("bench", b.TempDir(), nil)
			require.NoError(b, err)
			ctx, k, _ := newQueueTestKeeperWithDB(b, db)
			ctx = ctx.WithBlockTime(queueNow).WithBlockHeight(10)
			require.NoError(b, k.RecomputeQueueEarliest(ctx))
			for i := range 51 {
				if i == 50 && !bc.held {
					break
				}
				val, err := stakingtypes.NewValidator(sdk.ValAddress(PKs[i].Address()).String(), PKs[i], stakingtypes.Description{})
				require.NoError(b, err)
				val.Status = stakingtypes.Unbonding
				val.Tokens = math.NewInt(1)
				val.DelegatorShares = math.LegacyOneDec()
				val.UnbondingHeight = 5
				val.UnbondingTime = queueNow.Add(time.Duration(i+1) * time.Minute)
				if i == 50 {
					val.UnbondingTime = queueNow.Add(-time.Minute)
					val.UnbondingOnHoldRefCount = 1
				}
				require.NoError(b, k.SetValidator(ctx, val))
				require.NoError(b, k.InsertUnbondingValidatorQueue(ctx, val))
			}
			ctx.MultiStore().(storetypes.CommitMultiStore).Commit()
			unbond := k.UpstreamUnbondAllMatureValidators
			if bc.gated {
				unbond = k.UnbondAllMatureValidators
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cacheCtx, _ := ctx.CacheContext()
				if err := unbond(cacheCtx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
