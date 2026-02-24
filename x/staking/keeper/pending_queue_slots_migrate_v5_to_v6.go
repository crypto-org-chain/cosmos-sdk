package keeper

import (
	"context"
	"fmt"
	"time"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// PopulateValidatorQueuePendingFromIterator is used only by Migrate5to6 to seed the
// pending index from current queue state. End-block does not use the iterator.
func (k Keeper) PopulateValidatorQueuePendingFromIterator(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	iter, err := store.Iterator(types.ValidatorQueueKey, storetypes.PrefixEndBytes(types.ValidatorQueueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []TimeHeightQueueSlot
	for ; iter.Valid(); iter.Next() {
		keyTime, keyHeight, err := types.ParseValidatorQueueKey(iter.Key())
		if err != nil {
			return err
		}
		slots = append(slots, TimeHeightQueueSlot{Time: keyTime, Height: keyHeight})
	}
	return k.SetValidatorQueuePendingSlots(ctx, slots)
}

// populateTimeQueuePendingFromIterator is a generic function used by Migrate5to6 to populate
// time-only queue pending slots from iterator. End-block does not use the iterator.
func (k Keeper) populateTimeQueuePendingFromIterator(ctx context.Context, queueKey []byte, setter func(context.Context, []time.Time) error) error {
	store := k.storeService.OpenKVStore(ctx)
	iter, err := store.Iterator(queueKey, storetypes.PrefixEndBytes(queueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []time.Time
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) != len(queueKey) {
			return fmt.Errorf("invalid key length: expected %d, got %d", len(queueKey), len(key))

		}
		t, parseErr := sdk.ParseTimeBytes(iter.Value())
		if parseErr != nil {
			continue
		}
		slots = append(slots, t)
	}
	return setter(ctx, slots)
}

// PopulateRedelegationQueuePendingFromIterator is used only by Migrate5to6. End-block does not use the iterator.
func (k Keeper) PopulateUBDQueuePendingFromIterator(ctx context.Context) error {
	return k.populateTimeQueuePendingFromIterator(ctx, types.UnbondingQueueKey, k.SetUBDQueuePendingSlots)
}

// PopulateRedelegationQueuePendingFromIterator is used only by Migrate5to6. End-block does not use the iterator.
func (k Keeper) PopulateRedelegationQueuePendingFromIterator(ctx context.Context) error {
	return k.populateTimeQueuePendingFromIterator(ctx, types.RedelegationQueueKey, k.SetRedelegationQueuePendingSlots)
}
