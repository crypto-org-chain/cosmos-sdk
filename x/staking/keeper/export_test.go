package keeper

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// QueueEmptyMarker is the stored value for a queue known to be empty.
var QueueEmptyMarker = []byte(queueEmptyMarker)

// The ungated time-queue scans of upstream v0.55.0, copied verbatim apart from their names: the
// reference the gated scans are checked against.

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

// UpstreamUnbondAllMatureValidators runs the production validator loop ungated from the queue
// start, as upstream scans.
func (k Keeper) UpstreamUnbondAllMatureValidators(ctx context.Context) error {
	return k.unbondAllMatureValidators(ctx, types.ValidatorQueueKey, sdk.UnwrapSDKContext(ctx).BlockTime())
}
