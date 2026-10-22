package keeper

import (
	"context"
	"fmt"
	"time"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// The ungated end-block scans of upstream v0.55.0, copied verbatim apart from their names: the
// reference the gated scans are checked against. The validator copy is kept even though production
// keeps the same loop, so a change to that loop shows up as a difference from upstream.

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

func (k Keeper) UpstreamUnbondAllMatureValidators(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()
	blockHeight := sdkCtx.BlockHeight()

	// unbondingValIterator will contain all validator addresses indexed under
	// the ValidatorQueueKey prefix. Note, the entire index key is composed as
	// ValidatorQueueKey | timeBzLen (8-byte big endian) | timeBz | heightBz (8-byte big endian),
	// so it may be possible that certain validator addresses that are iterated
	// over are not ready to unbond, so an explicit check is required.
	unbondingValIterator, err := k.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	if err != nil {
		return err
	}
	defer unbondingValIterator.Close()

	for ; unbondingValIterator.Valid(); unbondingValIterator.Next() {
		key := unbondingValIterator.Key()
		keyTime, keyHeight, err := types.ParseValidatorQueueKey(key)
		if err != nil {
			return fmt.Errorf("failed to parse unbonding key: %w", err)
		}

		// All addresses for the given key have the same unbonding height and time.
		// We only unbond if the height and time are less than the current height
		// and time.
		if keyHeight <= blockHeight && (keyTime.Before(blockTime) || keyTime.Equal(blockTime)) {
			addrs := types.ValAddresses{}
			if err = k.cdc.Unmarshal(unbondingValIterator.Value(), &addrs); err != nil {
				return err
			}

			for _, valAddr := range addrs.Addresses {
				addr, err := k.validatorAddressCodec.StringToBytes(valAddr)
				if err != nil {
					return err
				}
				val, err := k.GetValidator(ctx, addr)
				if err != nil {
					return errorsmod.Wrap(err, "validator in the unbonding queue was not found")
				}

				if !val.IsUnbonding() {
					return fmt.Errorf("unexpected validator in unbonding queue; status was not unbonding")
				}

				if val.UnbondingOnHoldRefCount == 0 {
					for _, id := range val.UnbondingIds {
						if err = k.DeleteUnbondingIndex(ctx, id); err != nil {
							return err
						}
					}

					val, err = k.UnbondingToUnbonded(ctx, val)
					if err != nil {
						return err
					}

					if val.GetDelegatorShares().IsZero() {
						str, err := k.validatorAddressCodec.StringToBytes(val.GetOperator())
						if err != nil {
							return err
						}
						if err = k.RemoveValidator(ctx, str); err != nil {
							return err
						}
					} else {
						// remove unbonding ids
						val.UnbondingIds = []uint64{}
					}

					// remove validator from queue
					if err = k.DeleteValidatorQueue(ctx, val); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
