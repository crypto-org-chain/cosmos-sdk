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

// queueTestChain layers stores the way a node does: each block's writes go into a cache over the
// committed IAVL store, each tx into a cache nested in it, and the block is written and committed.
type queueTestChain struct {
	base sdk.Context
	k    *stakingkeeper.Keeper
	key  *storetypes.KVStoreKey
}

func newQueueTestChain(tb testing.TB) queueTestChain {
	tb.Helper()
	ctx, k, key := newQueueTestKeeper(tb)
	return queueTestChain{base: ctx, k: k, key: key}
}

// block opens the cache FinalizeBlock runs in; commit writes it and commits the store.
func (c queueTestChain) block(now time.Time, height int64) (ctx sdk.Context, commit func()) {
	ctx, write := c.base.WithBlockTime(now).WithBlockHeight(height).WithHeaderInfo(header.Info{Time: now, Height: height}).CacheContext()
	return ctx, func() {
		write()
		c.base.MultiStore().(storetypes.CommitMultiStore).Commit()
	}
}

// inTx runs fn in a cache nested in the block's, as DeliverTx does, and keeps its writes.
func inTx(tb testing.TB, blockCtx sdk.Context, fn func(sdk.Context) error) {
	tb.Helper()
	txCtx, write := blockCtx.CacheContext()
	require.NoError(tb, fn(txCtx))
	write()
}

// gatedSkip runs endBlock on a metered context and reports whether it read only the stored time,
// which is what a skipped scan costs. It fails if the stored time said the scan could be skipped
// but the gate scanned anyway.
func gatedSkip(tb testing.TB, ctx sdk.Context, key *storetypes.KVStoreKey, earliestKey []byte, now time.Time, endBlock func(sdk.Context) error) bool {
	tb.Helper()
	stored := ctx.KVStore(key).Get(earliestKey)
	notDue := bytes.Equal(stored, queueEmptyMarker) || (len(stored) > 1 && bytes.Compare(stored, sdk.FormatTimeBytes(now)) > 0)
	meteredCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	require.NoError(tb, endBlock(meteredCtx))
	if !notDue {
		return false
	}
	require.Equal(tb, earliestReadGas(meteredCtx.KVGasConfig(), len(stored)), meteredCtx.GasMeter().GasConsumed(), "scanned although not due")
	return true
}

// TestQueueEarliestMatchesUpstreamScan drives the gated keeper and upstream's ungated scans through
// the same random inserts and end-blocks, and checks they mature the same entries and keep the
// same queues, while the gated keeper's stored time never sorts after the queue's first key.
func TestQueueEarliestMatchesUpstreamScan(t *testing.T) {
	const seeds, steps = 200, 100
	odd := []time.Time{
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(20270, 1, 1, 0, 0, 0, 0, time.UTC),
		// Sorts after present-day keys, so it stays queued as an unsortable first entry.
		time.Date(20310, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	skipped := 0
	for seed := int64(0); seed < seeds; seed++ {
		r := rand.New(rand.NewSource(seed))
		gated, ref := newQueueTestChain(t), newQueueTestChain(t)
		now := queueNow
		var pending []time.Time
		for step := 0; step < steps; step++ {
			gatedCtx, commitGated := gated.block(now, int64(step+1))
			refCtx, commitRef := ref.block(now, int64(step+1))

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
				for _, c := range []struct {
					ctx sdk.Context
					k   *stakingkeeper.Keeper
				}{{gatedCtx, gated.k}, {refCtx, ref.k}} {
					inTx(t, c.ctx, func(txCtx sdk.Context) error {
						if toUBD {
							return c.k.InsertUBDQueue(txCtx, ubd, at)
						}
						return c.k.InsertRedelegationQueue(txCtx, red, at)
					})
				}
			}

			// Disturb only the gated keeper's stored times: an eager recompute, a fork v0.54.x list, a
			// lost value.
			switch r.Intn(25) {
			case 0:
				require.NoError(t, gated.k.RecomputeQueueEarliest(gatedCtx))
			case 1:
				gatedCtx.KVStore(gated.key).Set(stakingtypes.UBDQueueEarliestKey, legacyPendingSlots(1+uint32(r.Intn(3)), 8))
			case 2:
				gatedCtx.KVStore(gated.key).Delete(stakingtypes.RedelegationQueueEarliestKey)
			}

			var matureUBD []stakingtypes.DVPair
			if gatedSkip(t, gatedCtx, gated.key, stakingtypes.UBDQueueEarliestKey, now, func(ctx sdk.Context) (err error) {
				matureUBD, err = gated.k.DequeueAllMatureUBDQueue(ctx, now)
				return err
			}) {
				skipped++
			}
			refUBD, err := ref.k.UpstreamDequeueAllMatureUBDQueue(refCtx, now)
			require.NoError(t, err)
			require.Equal(t, entryStrings(refUBD), entryStrings(matureUBD), "seed %d step %d", seed, step)

			var matureRed []stakingtypes.DVVTriplet
			if gatedSkip(t, gatedCtx, gated.key, stakingtypes.RedelegationQueueEarliestKey, now, func(ctx sdk.Context) (err error) {
				matureRed, err = gated.k.DequeueAllMatureRedelegationQueue(ctx, time.Time{})
				return err
			}) {
				skipped++
			}
			refRed, err := ref.k.UpstreamDequeueAllMatureRedelegationQueue(refCtx, time.Time{})
			require.NoError(t, err)
			require.Equal(t, entryStrings(refRed), entryStrings(matureRed), "seed %d step %d", seed, step)

			commitGated()
			commitRef()
			for _, q := range timeQueues {
				require.Equal(t, dumpQueue(ref.base, ref.key, q.prefix), dumpQueue(gated.base, gated.key, q.prefix), "seed %d step %d", seed, step)
				requireEarliestBound(t, gated.base, gated.key, q.prefix, q.earliestKey, q.timeKey)
			}
			now = nextBlockTime(r, now, pending)
		}
	}
	// The comparison only covers the gate if it really skips scans.
	t.Logf("gate skipped %d of %d dequeues", skipped, 2*seeds*steps)
	require.Greater(t, skipped, 2*seeds*steps/4, "gate skipped too few dequeues")
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
	skipped, releases := 0, 0
	for seed := int64(0); seed < seeds; seed++ {
		r := rand.New(rand.NewSource(seed))
		gated, ref := newQueueTestChain(t), newQueueTestChain(t)
		now, height := queueNow, int64(10)
		var queued []stakingtypes.Validator
		next := 0
		for step := 0; step < steps; step++ {
			gatedCtx, commitGated := gated.block(now, height)
			refCtx, commitRef := ref.block(now, height)
			both := func(fn func(ctx sdk.Context, k *stakingkeeper.Keeper) error) {
				inTx(t, gatedCtx, func(ctx sdk.Context) error { return fn(ctx, gated.k) })
				inTx(t, refCtx, func(ctx sdk.Context) error { return fn(ctx, ref.k) })
			}

			// Release a hold, as an UnbondingCanComplete callback would.
			if r.Intn(2) == 0 {
				for _, queuedVal := range queued {
					valAddr, err := sdk.ValAddressFromBech32(queuedVal.GetOperator())
					require.NoError(t, err)
					val, err := gated.k.GetValidator(gatedCtx, valAddr)
					if err != nil || val.UnbondingOnHoldRefCount == 0 {
						continue // already removed, or not held
					}
					val.UnbondingOnHoldRefCount--
					both(func(ctx sdk.Context, k *stakingkeeper.Keeper) error { return k.SetValidator(ctx, val) })
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
				switch r.Intn(10) {
				case 0:
					val.UnbondingHeight = height + 1 + int64(r.Intn(3))
				case 1:
					// A chain started from a zero-height export queues unbonding validators at height 0.
					val.UnbondingHeight = 0
				default:
					val.UnbondingHeight = height - int64(r.Intn(5))
				}
				if r.Intn(8) == 0 {
					val.UnbondingOnHoldRefCount = 1
				}
				both(func(ctx sdk.Context, k *stakingkeeper.Keeper) error {
					if err := k.SetValidator(ctx, val); err != nil {
						return err
					}
					return k.InsertUnbondingValidatorQueue(ctx, val)
				})
				queued = append(queued, val)
			}
			// A validator that bonds again leaves the queue.
			if len(queued) > 0 && r.Intn(8) == 0 {
				val := queued[r.Intn(len(queued))]
				both(func(ctx sdk.Context, k *stakingkeeper.Keeper) error { return k.DeleteValidatorQueue(ctx, val) })
			}
			// Disturb only the gated keeper's stored time: an eager recompute, a fork v0.54.x list, a
			// lost value.
			switch r.Intn(25) {
			case 0:
				require.NoError(t, gated.k.RecomputeQueueEarliest(gatedCtx))
			case 1:
				gatedCtx.KVStore(gated.key).Set(stakingtypes.ValidatorQueueEarliestKey, legacyPendingSlots(1+uint32(r.Intn(3)), 16))
			case 2:
				gatedCtx.KVStore(gated.key).Delete(stakingtypes.ValidatorQueueEarliestKey)
			}

			if gatedSkip(t, gatedCtx, gated.key, stakingtypes.ValidatorQueueEarliestKey, now, func(ctx sdk.Context) error {
				return gated.k.UnbondAllMatureValidators(ctx)
			}) {
				skipped++
			}
			require.NoError(t, ref.k.UpstreamUnbondAllMatureValidators(refCtx))

			commitGated()
			commitRef()
			for _, prefix := range [][]byte{stakingtypes.ValidatorQueueKey, stakingtypes.ValidatorsKey} {
				require.Equal(t, dumpQueue(ref.base, ref.key, prefix), dumpQueue(gated.base, gated.key, prefix), "seed %d step %d", seed, step)
			}
			requireEarliestBound(t, gated.base, gated.key, stakingtypes.ValidatorQueueKey, stakingtypes.ValidatorQueueEarliestKey, validatorQueueKeyAt)

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
		require.NoError(t, err, "stored earliest is neither a time, the empty marker nor absent")
		if iter.Valid() {
			require.LessOrEqual(t, bytes.Compare(timeKey(storedTime), iter.Key()), 0, "stored time sorts after the first queue key")
		}
	}
}

// BenchmarkQueueEndBlock compares the unbonding queue end-block with upstream v0.55.0's scan on
// goleveldb, whose iterators don't read ahead the way MemDB's do, with 50 and 500 future
// timeslices. The after-5000-matured cases first mature and delete 5000 timeslices across
// committed versions; an iterator from the queue start still steps past those deleted keys, while
// the gated scan starts at the stored time. Each op also creates a CacheContext, which costs the
// same in both columns.
func BenchmarkQueueEndBlock(b *testing.B) {
	slice := func(n int) []stakingtypes.DVPair {
		return []stakingtypes.DVPair{{DelegatorAddress: fmt.Sprint("del", n), ValidatorAddress: "val"}}
	}
	for _, future := range []int{50, 500} {
		for _, bc := range []struct {
			name    string
			due     int // timeslices maturing this block
			matured int // timeslices matured and deleted in earlier committed versions
			gated   bool
		}{
			{"idle/upstream", 0, 0, false},
			{"idle/gated", 0, 0, true},
			{"due-1/upstream", 1, 0, false},
			{"due-1/gated", 1, 0, true},
			{"idle-after-5000-matured/upstream", 0, 5000, false},
			{"idle-after-5000-matured/gated", 0, 5000, true},
			{"due-1-after-5000-matured/upstream", 1, 5000, false},
			{"due-1-after-5000-matured/gated", 1, 5000, true},
		} {
			b.Run(fmt.Sprintf("future-%d/%s", future, bc.name), func(b *testing.B) {
				db, err := dbm.NewGoLevelDB("bench", b.TempDir(), nil)
				require.NoError(b, err)
				b.Cleanup(func() { require.NoError(b, db.Close()) })
				ctx, k, _ := newQueueTestKeeperWithDB(b, db)
				ctx = ctx.WithBlockTime(queueNow).WithHeaderInfo(header.Info{Time: queueNow})
				require.NoError(b, k.RecomputeQueueEarliest(ctx))
				for i := range future {
					require.NoError(b, k.SetUBDQueueTimeSlice(ctx, queueNow.Add(time.Duration(i+1)*time.Minute), slice(i)))
				}
				cms := ctx.MultiStore().(storetypes.CommitMultiStore)
				for batch := 0; batch < bc.matured; batch += 500 {
					for i := range 500 {
						require.NoError(b, k.SetUBDQueueTimeSlice(ctx, queueNow.Add(-time.Duration(batch+i+1)*time.Second), slice(i)))
					}
					cms.Commit()
					_, err := k.UpstreamDequeueAllMatureUBDQueue(ctx, queueNow)
					require.NoError(b, err)
					cms.Commit()
				}
				require.NoError(b, k.RecomputeQueueEarliest(ctx))
				for i := range bc.due {
					require.NoError(b, k.SetUBDQueueTimeSlice(ctx, queueNow.Add(-time.Duration(i+1)*time.Second), slice(i)))
				}
				cms.Commit()
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
			b.Cleanup(func() { require.NoError(b, db.Close()) })
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
