package keeper // noalias

import (
	"bytes"
	"context"
	"time"

	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ValidatorByPowerIndexExists does a certain by-power index record exist
func ValidatorByPowerIndexExists(ctx context.Context, keeper *Keeper, power []byte) bool {
	store := keeper.storeService.OpenKVStore(ctx)
	has, err := store.Has(power)
	if err != nil {
		panic(err)
	}
	return has
}

// TestingUpdateValidator updates a validator for testing
func TestingUpdateValidator(keeper *Keeper, ctx sdk.Context, validator types.Validator, apply bool) types.Validator {
	err := keeper.SetValidator(ctx, validator)
	if err != nil {
		panic(err)
	}

	// Remove any existing power key for validator.
	store := keeper.storeService.OpenKVStore(ctx)
	deleted := false

	iterator, err := store.Iterator(types.ValidatorsByPowerIndexKey, storetypes.PrefixEndBytes(types.ValidatorsByPowerIndexKey))
	if err != nil {
		panic(err)
	}
	defer iterator.Close()

	bz, err := keeper.validatorAddressCodec.StringToBytes(validator.GetOperator())
	if err != nil {
		panic(err)
	}

	for ; iterator.Valid(); iterator.Next() {
		valAddr := types.ParseValidatorPowerRankKey(iterator.Key())
		if bytes.Equal(valAddr, bz) {
			if deleted {
				panic("found duplicate power index key")
			} else {
				deleted = true
			}

			if err = store.Delete(iterator.Key()); err != nil {
				panic(err)
			}
		}
	}

	if err = keeper.SetValidatorByPowerIndex(ctx, validator); err != nil {
		panic(err)
	}

	if !apply {
		ctx, _ = ctx.CacheContext()
	}
	_, err = keeper.ApplyAndReturnValidatorSetUpdates(ctx)
	if err != nil {
		panic(err)
	}

	validator, err = keeper.GetValidator(ctx, sdk.ValAddress(bz))
	if err != nil {
		panic(err)
	}

	return validator
}

// Can be removed once migration v6 is complete

// SetValidatorQueueEntryPreV6Migration sets a validator queue entry in the old format (pre-migration)
// for testing migration functions. This writes directly to the store without updating pending slots.
func SetValidatorQueueEntryPreV6Migration(keeper *Keeper, ctx context.Context, endTime time.Time, endHeight int64, addrs []string) error {
	store := keeper.storeService.OpenKVStore(ctx)
	bz, err := keeper.cdc.Marshal(&types.ValAddresses{Addresses: addrs})
	if err != nil {
		return err
	}
	return store.Set(types.GetValidatorQueueKey(endTime, endHeight), bz)
}

// SetUBDQueueEntryPreV6Migration sets a UBD queue entry in the old format (pre-migration)
// for testing migration functions. The value is time bytes, not marshaled DVPairs.
func SetUBDQueueEntryPreV6Migration(keeper *Keeper, ctx context.Context, timestamp time.Time) error {
	store := keeper.storeService.OpenKVStore(ctx)
	timeBz := sdk.FormatTimeBytes(timestamp)
	return store.Set(types.GetUnbondingDelegationTimeKey(timestamp), timeBz)
}

// SetRedelegationQueueEntryPreV6Migration sets a redelegation queue entry in the old format (pre-migration)
// for testing migration functions. The value is time bytes, not marshaled DVVTriplets.
func SetRedelegationQueueEntryPreV6Migration(keeper *Keeper, ctx context.Context, timestamp time.Time) error {
	store := keeper.storeService.OpenKVStore(ctx)
	timeBz := sdk.FormatTimeBytes(timestamp)
	return store.Set(types.GetRedelegationTimeKey(timestamp), timeBz)
}
