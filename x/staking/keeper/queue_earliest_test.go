package keeper_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/core/header"
	sdkmath "cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtestutil "github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var (
	queueNow          = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	queueEmptyMarker  = stakingkeeper.QueueEmptyMarker
	unknownValAddress = sdk.ValAddress(PKs[0].Address()).String()
)

// timeQueue drives the unbonding and redelegation queues, which share one shape.
type timeQueue struct {
	name        string
	prefix      []byte
	earliestKey []byte
	timeKey     func(time.Time) []byte
	slice       func() []byte
	insert      func(s *KeeperTestSuite, ctx sdk.Context, t time.Time) error
	dequeue     func(s *KeeperTestSuite, ctx sdk.Context) (int, error)
}

var timeQueues = []timeQueue{
	{
		name:        "unbonding",
		prefix:      stakingtypes.UnbondingQueueKey,
		earliestKey: stakingtypes.UBDQueueEarliestKey,
		timeKey:     stakingtypes.GetUnbondingDelegationTimeKey,
		slice: func() []byte {
			bz, err := (&stakingtypes.DVPairs{Pairs: []stakingtypes.DVPair{{DelegatorAddress: "del", ValidatorAddress: "val"}}}).Marshal()
			if err != nil {
				panic(err)
			}
			return bz
		},
		insert: func(s *KeeperTestSuite, ctx sdk.Context, t time.Time) error {
			return s.stakingKeeper.SetUBDQueueTimeSlice(ctx, t, []stakingtypes.DVPair{{DelegatorAddress: "del", ValidatorAddress: "val"}})
		},
		dequeue: func(s *KeeperTestSuite, ctx sdk.Context) (int, error) {
			mature, err := s.stakingKeeper.DequeueAllMatureUBDQueue(ctx, ctx.BlockTime())
			return len(mature), err
		},
	},
	{
		name:        "redelegation",
		prefix:      stakingtypes.RedelegationQueueKey,
		earliestKey: stakingtypes.RedelegationQueueEarliestKey,
		timeKey:     stakingtypes.GetRedelegationTimeKey,
		slice: func() []byte {
			bz, err := (&stakingtypes.DVVTriplets{Triplets: []stakingtypes.DVVTriplet{{DelegatorAddress: "del", ValidatorSrcAddress: "src", ValidatorDstAddress: "dst"}}}).Marshal()
			if err != nil {
				panic(err)
			}
			return bz
		},
		insert: func(s *KeeperTestSuite, ctx sdk.Context, t time.Time) error {
			return s.stakingKeeper.SetRedelegationQueueTimeSlice(ctx, t, []stakingtypes.DVVTriplet{{DelegatorAddress: "del", ValidatorSrcAddress: "src", ValidatorDstAddress: "dst"}})
		},
		dequeue: func(s *KeeperTestSuite, ctx sdk.Context) (int, error) {
			mature, err := s.stakingKeeper.DequeueAllMatureRedelegationQueue(ctx, ctx.BlockTime())
			return len(mature), err
		},
	},
}

// requireOnlyEarliestRead requires that the end-block run on gasCtx read only the stored earliest
// time, which shows its gate skipped the scan.
func requireOnlyEarliestRead(tb testing.TB, gasCtx sdk.Context, stored []byte) {
	tb.Helper()
	require.Equal(tb, earliestReadGas(gasCtx.KVGasConfig(), len(stored)), gasCtx.GasMeter().GasConsumed(), "scanned although not due")
}

// earliestWrites counts the writes run makes to key, using the multistore's change listener.
func (s *KeeperTestSuite) earliestWrites(ctx sdk.Context, key []byte, run func()) int {
	cms, ok := ctx.MultiStore().(storetypes.CommitMultiStore)
	s.Require().True(ok)
	cms.AddListeners([]storetypes.StoreKey{s.storeKey})
	cms.PopStateCache()
	run()
	writes := 0
	for _, pair := range cms.PopStateCache() {
		if bytes.Equal(pair.Key, key) {
			writes++
		}
	}
	return writes
}

func atQueueTime(ctx sdk.Context, t time.Time) sdk.Context {
	return ctx.WithBlockTime(t).WithHeaderInfo(header.Info{Time: t})
}

// legacyPendingSlots encodes count slots in the fork v0.54.x pending-slot list format.
func legacyPendingSlots(count uint32, slotBytes int) []byte {
	bz := make([]byte, 4+int(count)*slotBytes)
	binary.BigEndian.PutUint32(bz, count)
	for i := range int(count) {
		binary.BigEndian.PutUint64(bz[4+i*slotBytes:], uint64(queueNow.Add(-time.Duration(i+1)*time.Hour).UnixNano()))
	}
	return bz
}

// earliestReadGas is the gas of reading a valueLen-byte value from a one-byte earliest-time key.
func earliestReadGas(cfg storetypes.GasConfig, valueLen int) storetypes.Gas {
	return cfg.ReadCostFlat + cfg.ReadCostPerByte*storetypes.Gas(1+valueLen)
}

func (s *KeeperTestSuite) TestTimeQueueEarliestGate() {
	hour := time.Hour
	testCases := []struct {
		name        string
		malleate    func(q timeQueue, ctx sdk.Context, store storetypes.KVStore)
		now         time.Time
		expMatured  int
		expEarliest []byte
	}{
		{
			name: "due scan starts at the stored time",
			malleate: func(q timeQueue, ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(-hour)))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
				// Bypasses the gate and sorts before the stored time, so only a scan from the queue
				// start would find it.
				store.Set(q.timeKey(queueNow.Add(-2*hour)), q.slice())
			},
			now:         queueNow,
			expMatured:  1,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			// Fork v0.54.x encoded times as UnixNano, which can't represent them.
			name: "times past year 2262 mature",
			malleate: func(q timeQueue, ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().NoError(q.insert(s, ctx, time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)))
			},
			now:         time.Date(2300, 1, 1, 0, 0, 1, 0, time.UTC),
			expMatured:  1,
			expEarliest: queueEmptyMarker,
		},
	}

	for _, q := range timeQueues {
		for _, tc := range testCases {
			s.Run(q.name+"/"+tc.name, func() {
				s.SetupTest()
				ctx := atQueueTime(s.ctx, tc.now)
				store := ctx.KVStore(s.storeKey)
				tc.malleate(q, ctx, store)

				matured, err := q.dequeue(s, ctx)
				s.Require().NoError(err)
				s.Require().Equal(tc.expMatured, matured)
				s.Require().Equal(tc.expEarliest, store.Get(q.earliestKey))
			})
		}
	}
}

func (s *KeeperTestSuite) TestTimeQueueInsertGas() {
	cfg := s.ctx.KVGasConfig()
	readGas := func(valueLen int) storetypes.Gas { return earliestReadGas(cfg, valueLen) }
	writeGas := func(valueLen int) storetypes.Gas {
		return cfg.WriteCostFlat + cfg.WriteCostPerByte*storetypes.Gas(1+valueLen)
	}
	timeLen := len(sdk.FormatTimeBytes(queueNow))
	hour := time.Hour
	insertAt := queueNow.Add(hour)

	testCases := []struct {
		name     string
		malleate func(q timeQueue, ctx sdk.Context)
		expGas   storetypes.Gas // gas the gate adds to an insert at insertAt
	}{
		{
			// One pending completion time per block is what drives the count up on a live chain.
			name: "later insert with 1000 pending times",
			malleate: func(q timeQueue, ctx sdk.Context) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				for i := range 1000 {
					s.Require().NoError(q.insert(s, ctx, queueNow.Add(-time.Duration(i)*time.Second)))
				}
			},
			expGas: readGas(timeLen),
		},
		{
			name: "earlier insert lowers the stored time",
			malleate: func(q timeQueue, ctx sdk.Context) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(2*hour)))
			},
			expGas: readGas(timeLen) + writeGas(timeLen),
		},
		{
			name: "insert into an empty queue",
			malleate: func(_ timeQueue, ctx sdk.Context) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
			},
			expGas: readGas(len(queueEmptyMarker)) + writeGas(timeLen),
		},
		{
			name:     "insert with the stored time unknown",
			malleate: func(timeQueue, sdk.Context) {},
			expGas:   readGas(0),
		},
	}

	for _, q := range timeQueues {
		for _, tc := range testCases {
			s.Run(q.name+"/"+tc.name, func() {
				s.SetupTest()
				ctx := atQueueTime(s.ctx, queueNow)
				tc.malleate(q, ctx)
				gasOf := func(fn func(sdk.Context)) storetypes.Gas {
					cacheCtx, _ := ctx.CacheContext()
					gasCtx := cacheCtx.WithGasMeter(storetypes.NewGasMeter(math.MaxUint64))
					fn(gasCtx)
					return gasCtx.GasMeter().GasConsumed()
				}
				insertGas := gasOf(func(c sdk.Context) { s.Require().NoError(q.insert(s, c, insertAt)) })
				// The same write without the gate, as upstream's setter makes it.
				rawGas := gasOf(func(c sdk.Context) { c.KVStore(s.storeKey).Set(q.timeKey(insertAt), q.slice()) })
				s.Require().Equal(tc.expGas, insertGas-rawGas)
			})
		}
	}
}

func (s *KeeperTestSuite) TestQueueEarliestSkipsUnchangedWrites() {
	hour := time.Hour
	for _, q := range timeQueues {
		s.Run(q.name+"/insert at the stored time", func() {
			s.SetupTest()
			ctx := atQueueTime(s.ctx, queueNow)
			s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
			s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
			s.Require().Zero(s.earliestWrites(ctx, q.earliestKey, func() {
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
			}))
		})
	}
	s.Run("validator/due scan that keeps the same earliest entry", func() {
		s.SetupTest()
		ctx := atQueueTime(s.ctx, queueNow).WithBlockHeight(10)
		s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
		// Due by time but not by height, so every block scans it and keeps it.
		s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(-hour), 20, []string{unknownValAddress}))
		s.Require().Zero(s.earliestWrites(ctx, stakingtypes.ValidatorQueueEarliestKey, func() {
			s.Require().NoError(s.stakingKeeper.UnbondAllMatureValidators(ctx))
		}))
	})
}

func (s *KeeperTestSuite) TestQueueTimesOutsideSortableRange() {
	hour := time.Hour
	farFuture := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	negativeYear := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	ubd := timeQueues[0]
	testCases := []struct {
		name        string
		malleate    func(ctx sdk.Context, store storetypes.KVStore)
		endBlock    func(ctx sdk.Context) (int, error)
		earliestKey []byte
		expMatured  int
		expEarliest []byte
	}{
		{
			name: "validator insert past year 9999 makes the earliest time unknown",
			malleate: func(ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, farFuture, 1, []string{unknownValAddress}))
				s.Require().Nil(store.Get(stakingtypes.ValidatorQueueEarliestKey))
			},
			endBlock:    func(ctx sdk.Context) (int, error) { return 0, s.stakingKeeper.UnbondAllMatureValidators(ctx) },
			earliestKey: stakingtypes.ValidatorQueueEarliestKey,
		},
		{
			name: "validator insert before year 0 makes a known earliest time unknown",
			malleate: func(ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(hour), 1, []string{unknownValAddress}))
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, negativeYear, 1, []string{unknownValAddress}))
				s.Require().Nil(store.Get(stakingtypes.ValidatorQueueEarliestKey))
			},
			// Its longer time bytes sort it after every other key, out of the scan's bound as in
			// upstream, so one scan finds the +1h entry first again.
			endBlock:    func(ctx sdk.Context) (int, error) { return 0, s.stakingKeeper.UnbondAllMatureValidators(ctx) },
			earliestKey: stakingtypes.ValidatorQueueEarliestKey,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "recompute clears the empty marker when the only entry is unsortable",
			malleate: func(ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().Equal(queueEmptyMarker, store.Get(stakingtypes.ValidatorQueueEarliestKey))
				s.setRawValidatorQueue(store, farFuture)
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
			},
			endBlock:    func(sdk.Context) (int, error) { return 0, nil },
			earliestKey: stakingtypes.ValidatorQueueEarliestKey,
		},
		{
			name: "recompute clears a known time when the first entry is unsortable",
			malleate: func(ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				s.Require().NoError(ubd.insert(s, ctx, queueNow.Add(hour)))
				// "10000-..." sorts before "2030-...", so it becomes the first entry.
				store.Set(ubd.timeKey(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)), ubd.slice())
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
			},
			endBlock:    func(sdk.Context) (int, error) { return 0, nil },
			earliestKey: ubd.earliestKey,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			ctx := atQueueTime(s.ctx, queueNow).WithBlockHeight(10)
			store := ctx.KVStore(s.storeKey)
			tc.malleate(ctx, store)

			matured, err := tc.endBlock(ctx)
			s.Require().NoError(err)
			s.Require().Equal(tc.expMatured, matured)
			s.Require().Equal(tc.expEarliest, store.Get(tc.earliestKey))
		})
	}
}

func (s *KeeperTestSuite) TestValidatorGateScansPastYear9999() {
	ctx := atQueueTime(s.ctx, queueNow).WithBlockHeight(10)
	s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
	s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(time.Hour), 1, []string{unknownValAddress}))

	// Upstream's scan bound for a 30-byte block time sorts after every 29-byte key, so the entry is
	// due, although "2030-..." sorts after "10000-..." byte by byte. The scan fails on it.
	err := s.stakingKeeper.UnbondAllMatureValidators(atQueueTime(ctx, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)))
	s.Require().ErrorIs(err, stakingtypes.ErrNoValidatorFound)
}

func (s *KeeperTestSuite) TestPopulateQueuePendingSlotsAlias() {
	ctx := atQueueTime(s.ctx, queueNow)
	store := ctx.KVStore(s.storeKey)
	store.Set(timeQueues[0].earliestKey, legacyPendingSlots(1, 8))
	s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
	s.Require().Equal(queueEmptyMarker, store.Get(timeQueues[0].earliestKey))
}

func (s *KeeperTestSuite) TestMigrate5to6RecomputesQueueEarliest() {
	ctx := atQueueTime(s.ctx, queueNow)
	store := ctx.KVStore(s.storeKey)
	q := timeQueues[0]
	store.Set(q.earliestKey, legacyPendingSlots(1, 8))
	store.Set(q.timeKey(queueNow.Add(-time.Hour)), q.slice())

	s.Require().NoError(stakingkeeper.NewMigrator(s.stakingKeeper).Migrate5to6(ctx))
	s.Require().Equal(sdk.FormatTimeBytes(queueNow.Add(-time.Hour)), store.Get(q.earliestKey))
}

func (s *KeeperTestSuite) TestValidatorQueueEarliestGate() {
	hour := time.Hour
	testCases := []struct {
		name        string
		malleate    func(ctx sdk.Context, store storetypes.KVStore)
		expEarliest []byte
	}{
		{
			name: "due scan starts at the stored time",
			malleate: func(ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
				// Due by time but not by height, so it alone doesn't fail the scan.
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(-hour), 20, []string{unknownValAddress}))
				// Bypasses the gate and sorts before the stored time; only a scan from the queue
				// start would reach it and fail.
				s.setRawValidatorQueue(store, queueNow.Add(-2*hour))
			},
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(-hour)),
		},
		{
			name:        "empty queue records the empty marker",
			malleate:    func(sdk.Context, storetypes.KVStore) {},
			expEarliest: queueEmptyMarker,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			ctx := atQueueTime(s.ctx, queueNow).WithBlockHeight(10)
			store := ctx.KVStore(s.storeKey)
			tc.malleate(ctx, store)

			s.Require().NoError(s.stakingKeeper.UnbondAllMatureValidators(ctx))
			s.Require().Equal(tc.expEarliest, store.Get(stakingtypes.ValidatorQueueEarliestKey))
		})
	}
}

func (s *KeeperTestSuite) TestUnbondingQueueGateUsesCurrTime() {
	// Unlike redelegations, unbonding delegations mature by the currTime argument, so the gate must
	// use it too, not the block time.
	ctx := atQueueTime(s.ctx, queueNow)
	s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
	s.Require().NoError(timeQueues[0].insert(s, ctx, queueNow.Add(time.Hour)))

	matured, err := s.stakingKeeper.DequeueAllMatureUBDQueue(ctx, queueNow.Add(2*time.Hour))
	s.Require().NoError(err)
	s.Require().Len(matured, 1)
}

func (s *KeeperTestSuite) TestRedelegationQueueGateUsesHeaderTime() {
	q := timeQueues[1]
	// The redelegation scan is bounded by HeaderInfo().Time, not its currTime argument, so the
	// gate must be too: one on the later block time would scan although nothing matures.
	ctx := s.ctx.WithBlockTime(queueNow).WithHeaderInfo(header.Info{Time: queueNow.Add(-2 * time.Hour)})
	store := ctx.KVStore(s.storeKey)
	s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
	s.Require().NoError(q.insert(s, ctx, queueNow.Add(-time.Hour)))

	gasCtx := ctx.WithGasMeter(storetypes.NewGasMeter(math.MaxUint64))
	matured, err := q.dequeue(s, gasCtx)
	s.Require().NoError(err)
	s.Require().Zero(matured)
	stored := sdk.FormatTimeBytes(queueNow.Add(-time.Hour))
	s.Require().Equal(stored, store.Get(q.earliestKey))
	requireOnlyEarliestRead(s.T(), gasCtx, stored)
}

func (s *KeeperTestSuite) TestValidatorScanSeesEntriesQueuedDuringIt() {
	hour := time.Hour
	ctx := atQueueTime(s.ctx, queueNow).WithBlockHeight(10)
	s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))

	val, err := stakingtypes.NewValidator(sdk.ValAddress(PKs[1].Address()).String(), PKs[1], stakingtypes.Description{})
	s.Require().NoError(err)
	val.Status = stakingtypes.Unbonding
	val.Tokens = sdkmath.ZeroInt()
	val.DelegatorShares = sdkmath.LegacyZeroDec()
	val.UnbondingHeight = 5
	val.UnbondingTime = queueNow.Add(-hour)
	s.Require().NoError(s.stakingKeeper.SetValidator(ctx, val))
	s.Require().NoError(s.stakingKeeper.InsertUnbondingValidatorQueue(ctx, val))

	// Removing the matured validator queues an entry below the stored time. It is due by time but
	// not by height, so it stays queued.
	hooks := stakingtestutil.NewMockStakingHooks(gomock.NewController(s.T()))
	hooks.EXPECT().AfterValidatorRemoved(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ sdk.ConsAddress, _ sdk.ValAddress) error {
			return s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(-2*hour), 20, []string{unknownValAddress})
		})
	s.stakingKeeper.SetHooks(hooks)

	writes := s.earliestWrites(ctx, stakingtypes.ValidatorQueueEarliestKey, func() {
		s.Require().NoError(s.stakingKeeper.UnbondAllMatureValidators(ctx))
	})
	s.Require().Equal(sdk.FormatTimeBytes(queueNow.Add(-2*hour)), ctx.KVStore(s.storeKey).Get(stakingtypes.ValidatorQueueEarliestKey))
	// Only the hook's insert writes it: the scan then finds that time already stored.
	s.Require().Equal(1, writes)
}

func (s *KeeperTestSuite) setRawValidatorQueue(store storetypes.KVStore, t time.Time) {
	bz, err := (&stakingtypes.ValAddresses{Addresses: []string{unknownValAddress}}).Marshal()
	s.Require().NoError(err)
	store.Set(stakingtypes.GetValidatorQueueKey(t, 1), bz)
}

func (s *KeeperTestSuite) TestRecomputeQueueEarliest() {
	ctx := atQueueTime(s.ctx, queueNow)
	store := ctx.KVStore(s.storeKey)
	first := queueNow.Add(time.Hour)

	store.Set(stakingtypes.ValidatorQueueEarliestKey, legacyPendingSlots(1, 16))
	s.setRawValidatorQueue(store, first)
	s.setRawValidatorQueue(store, first.Add(time.Hour))
	store.Set(timeQueues[0].earliestKey, legacyPendingSlots(1, 8))
	store.Set(timeQueues[0].timeKey(first.Add(time.Hour)), timeQueues[0].slice())
	store.Set(timeQueues[0].timeKey(first), timeQueues[0].slice())
	// The redelegation queue is left empty.

	expected := map[string][]byte{
		string(stakingtypes.ValidatorQueueEarliestKey):    sdk.FormatTimeBytes(first),
		string(stakingtypes.UBDQueueEarliestKey):          sdk.FormatTimeBytes(first),
		string(stakingtypes.RedelegationQueueEarliestKey): queueEmptyMarker,
	}
	s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
	for key, value := range expected {
		s.Require().Equal(value, store.Get([]byte(key)))
		// Running it again, as an upgrade handler may after end-block recorded the times, rewrites
		// nothing.
		s.Require().Zero(s.earliestWrites(ctx, []byte(key), func() {
			s.Require().NoError(s.stakingKeeper.RecomputeQueueEarliest(ctx))
		}))
		s.Require().Equal(value, store.Get([]byte(key)))
	}
}
