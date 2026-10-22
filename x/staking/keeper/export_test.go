package keeper

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// The ungated time-queue end-block scans of upstream v0.55.0, copied verbatim apart from their
// names, so the model tests and benchmarks can compare the gated scans against them.

func (k Keeper) UpstreamDequeueAllMatureUBDQueue(ctx context.Context, currTime time.Time) (matureUnbonds []types.DVPair, err error) {
	store := k.storeService.OpenKVStore(ctx)

	// gets an iterator for all timeslices from time 0 until the current Blockheader time
	unbondingTimesliceIterator, err := k.UBDQueueIterator(ctx, currTime)
	if err != nil {
		return matureUnbonds, err
	}
	defer unbondingTimesliceIterator.Close()

	for ; unbondingTimesliceIterator.Valid(); unbondingTimesliceIterator.Next() {
		timeslice := types.DVPairs{}
		value := unbondingTimesliceIterator.Value()
		if err = k.cdc.Unmarshal(value, &timeslice); err != nil {
			return matureUnbonds, err
		}

		matureUnbonds = append(matureUnbonds, timeslice.Pairs...)

		if err = store.Delete(unbondingTimesliceIterator.Key()); err != nil {
			return matureUnbonds, err
		}

	}

	return matureUnbonds, nil
}

func (k Keeper) UpstreamDequeueAllMatureRedelegationQueue(ctx context.Context, currTime time.Time) (matureRedelegations []types.DVVTriplet, err error) {
	store := k.storeService.OpenKVStore(ctx)

	// gets an iterator for all timeslices from time 0 until the current Blockheader time
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	redelegationTimesliceIterator, err := k.RedelegationQueueIterator(ctx, sdkCtx.HeaderInfo().Time)
	if err != nil {
		return nil, err
	}
	defer redelegationTimesliceIterator.Close()

	for ; redelegationTimesliceIterator.Valid(); redelegationTimesliceIterator.Next() {
		timeslice := types.DVVTriplets{}
		value := redelegationTimesliceIterator.Value()
		if err = k.cdc.Unmarshal(value, &timeslice); err != nil {
			return nil, err
		}

		matureRedelegations = append(matureRedelegations, timeslice.Triplets...)

		if err = store.Delete(redelegationTimesliceIterator.Key()); err != nil {
			return nil, err
		}
	}

	return matureRedelegations, nil
}

// UpstreamUnbondAllMatureValidators runs upstream's validator scan, whose body is kept verbatim in
// unbondAllMatureValidators apart from taking the block time as a parameter.
func (k Keeper) UpstreamUnbondAllMatureValidators(ctx context.Context) error {
	return k.unbondAllMatureValidators(ctx, sdk.UnwrapSDKContext(ctx).BlockTime())
}
