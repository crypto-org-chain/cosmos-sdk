package keeper_test

import (
	"encoding/binary"
	"math"
	"time"

	"cosmossdk.io/core/header"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var (
	queueNow          = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	queueEmptyMarker  = []byte{0}
	unknownValAddress = sdk.ValAddress(PKs[0].Address()).String()
)

// timeQueue drives the unbonding and redelegation queues, which share one shape.
type timeQueue struct {
	name        string
	earliestKey []byte
	timeKey     func(time.Time) []byte
	slice       func() []byte
	insert      func(s *KeeperTestSuite, ctx sdk.Context, t time.Time) error
	dequeue     func(s *KeeperTestSuite, ctx sdk.Context) (int, error)
}

var timeQueues = []timeQueue{
	{
		name:        "unbonding",
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

func atQueueTime(ctx sdk.Context, t time.Time) sdk.Context {
	return ctx.WithBlockTime(t).WithHeaderInfo(header.Info{Time: t})
}

// legacyPendingSlots encodes one slot in the fork v0.54.x pending-slot list format.
func legacyPendingSlots(slotBytes int) []byte {
	bz := make([]byte, 4+slotBytes)
	binary.BigEndian.PutUint32(bz, 1)
	binary.BigEndian.PutUint64(bz[4:], uint64(queueNow.Add(-time.Hour).UnixNano()))
	return bz
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
			name: "earliest in the future skips the scan",
			malleate: func(q timeQueue, ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
				// Bypasses the gate, so only a scan would find it.
				store.Set(q.timeKey(queueNow.Add(-hour)), q.slice())
			},
			now:         queueNow,
			expMatured:  0,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "due earliest scans and records the next entry",
			malleate: func(q timeQueue, ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(-hour)))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
			},
			now:         queueNow,
			expMatured:  1,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "draining the queue records the empty marker",
			malleate: func(q timeQueue, ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(q.insert(s, ctx, queueNow))
			},
			now:         queueNow,
			expMatured:  1,
			expEarliest: queueEmptyMarker,
		},
		{
			name: "earlier insert lowers the earliest and a later one keeps it",
			malleate: func(q timeQueue, ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(2*hour)))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(3*hour)))
			},
			now:         queueNow,
			expMatured:  0,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "missing earliest stays unknown on insert and is found by one scan",
			malleate: func(q timeQueue, ctx sdk.Context, store storetypes.KVStore) {
				store.Set(q.timeKey(queueNow.Add(-hour)), q.slice())
				s.Require().NoError(q.insert(s, ctx, queueNow.Add(hour)))
				s.Require().Nil(store.Get(q.earliestKey))
			},
			now:         queueNow,
			expMatured:  1,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "fork v0.54.x pending-slot list is replaced",
			malleate: func(q timeQueue, _ sdk.Context, store storetypes.KVStore) {
				store.Set(q.earliestKey, legacyPendingSlots(8))
				store.Set(q.timeKey(queueNow.Add(-hour)), q.slice())
			},
			now:         queueNow,
			expMatured:  1,
			expEarliest: queueEmptyMarker,
		},
		{
			name: "stale earliest whose timeslice is gone is recomputed",
			malleate: func(q timeQueue, _ sdk.Context, store storetypes.KVStore) {
				store.Set(q.earliestKey, sdk.FormatTimeBytes(queueNow.Add(-hour)))
				store.Set(q.timeKey(queueNow.Add(hour)), q.slice())
			},
			now:         queueNow,
			expMatured:  0,
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "times outside the UnixNano range mature",
			malleate: func(q timeQueue, ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
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

func (s *KeeperTestSuite) TestTimeQueueInsertGasIsConstant() {
	for _, q := range timeQueues {
		s.Run(q.name, func() {
			insertGas := func(pending int) storetypes.Gas {
				s.SetupTest()
				ctx := atQueueTime(s.ctx, queueNow)
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				for i := range pending {
					s.Require().NoError(q.insert(s, ctx, queueNow.Add(time.Duration(i+1)*time.Second)))
				}
				gasCtx := ctx.WithGasMeter(storetypes.NewGasMeter(math.MaxUint64))
				s.Require().NoError(q.insert(s, gasCtx, queueNow.Add(time.Duration(pending+1)*time.Second)))
				return gasCtx.GasMeter().GasConsumed()
			}
			// One pending completion time per block is what drives the count up on a live chain.
			s.Require().Equal(insertGas(1), insertGas(1000))
		})
	}
}

func (s *KeeperTestSuite) TestValidatorQueueEarliestGate() {
	hour := time.Hour
	testCases := []struct {
		name        string
		malleate    func(ctx sdk.Context, store storetypes.KVStore)
		expErr      error
		expEarliest []byte
	}{
		{
			name: "earliest in the future skips the scan",
			malleate: func(ctx sdk.Context, store storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(hour), 1, []string{unknownValAddress}))
				// Bypasses the gate; a scan would fail on the unknown validator.
				s.setRawValidatorQueue(store, queueNow.Add(-hour))
			},
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "due earliest scans the queue",
			malleate: func(ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(-hour), 1, []string{unknownValAddress}))
			},
			expErr: stakingtypes.ErrNoValidatorFound,
		},
		{
			name: "missing earliest scans the queue",
			malleate: func(_ sdk.Context, store storetypes.KVStore) {
				s.setRawValidatorQueue(store, queueNow.Add(-hour))
			},
			expErr: stakingtypes.ErrNoValidatorFound,
		},
		{
			name: "fork v0.54.x pending-slot list is replaced",
			malleate: func(_ sdk.Context, store storetypes.KVStore) {
				store.Set(stakingtypes.ValidatorQueueEarliestKey, legacyPendingSlots(16))
				s.setRawValidatorQueue(store, queueNow.Add(hour))
			},
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "earlier insert lowers the earliest and a later one keeps it",
			malleate: func(ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				for _, t := range []time.Time{queueNow.Add(2 * hour), queueNow.Add(hour), queueNow.Add(3 * hour)} {
					s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, t, 1, []string{unknownValAddress}))
				}
			},
			expEarliest: sdk.FormatTimeBytes(queueNow.Add(hour)),
		},
		{
			name: "entry due by time but not by height stays queued and due",
			malleate: func(ctx sdk.Context, _ storetypes.KVStore) {
				s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
				s.Require().NoError(s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, queueNow.Add(-hour), 20, []string{unknownValAddress}))
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

			err := s.stakingKeeper.UnbondAllMatureValidators(ctx)
			if tc.expErr != nil {
				s.Require().ErrorIs(err, tc.expErr)
				return
			}
			s.Require().NoError(err)
			s.Require().Equal(tc.expEarliest, store.Get(stakingtypes.ValidatorQueueEarliestKey))
		})
	}
}

func (s *KeeperTestSuite) TestRedelegationQueueGateUsesHeaderTime() {
	q := timeQueues[1]
	// The redelegation scan is bounded by HeaderInfo().Time, not its currTime argument, so the
	// gate must be too: a gate on the later block time would scan and dequeue the bypassed entry.
	ctx := s.ctx.WithBlockTime(queueNow).WithHeaderInfo(header.Info{Time: queueNow.Add(-2 * time.Hour)})
	store := ctx.KVStore(s.storeKey)
	s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
	s.Require().NoError(q.insert(s, ctx, queueNow.Add(-time.Hour)))
	store.Set(q.timeKey(queueNow.Add(-3*time.Hour)), q.slice())

	matured, err := q.dequeue(s, ctx)
	s.Require().NoError(err)
	s.Require().Zero(matured)
	s.Require().Equal(sdk.FormatTimeBytes(queueNow.Add(-time.Hour)), store.Get(q.earliestKey))
}

func (s *KeeperTestSuite) setRawValidatorQueue(store storetypes.KVStore, t time.Time) {
	bz, err := (&stakingtypes.ValAddresses{Addresses: []string{unknownValAddress}}).Marshal()
	s.Require().NoError(err)
	store.Set(stakingtypes.GetValidatorQueueKey(t, 1), bz)
}

func (s *KeeperTestSuite) TestPopulateQueuePendingSlots() {
	ctx := atQueueTime(s.ctx, queueNow)
	store := ctx.KVStore(s.storeKey)
	first := queueNow.Add(time.Hour)

	store.Set(stakingtypes.ValidatorQueueEarliestKey, legacyPendingSlots(16))
	s.setRawValidatorQueue(store, first)
	s.setRawValidatorQueue(store, first.Add(time.Hour))
	store.Set(timeQueues[0].earliestKey, legacyPendingSlots(8))
	store.Set(timeQueues[0].timeKey(first.Add(time.Hour)), timeQueues[0].slice())
	store.Set(timeQueues[0].timeKey(first), timeQueues[0].slice())
	// The redelegation queue is left empty.

	expected := map[string][]byte{
		string(stakingtypes.ValidatorQueueEarliestKey):    sdk.FormatTimeBytes(first),
		string(stakingtypes.UBDQueueEarliestKey):          sdk.FormatTimeBytes(first),
		string(stakingtypes.RedelegationQueueEarliestKey): queueEmptyMarker,
	}
	// Upgrade handlers may call it on a chain whose end-block already recorded the times.
	for range 2 {
		s.Require().NoError(s.stakingKeeper.PopulateQueuePendingSlots(ctx))
		for key, value := range expected {
			s.Require().Equal(value, store.Get([]byte(key)))
		}
	}
}
