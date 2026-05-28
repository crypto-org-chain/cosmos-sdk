package keeper

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	corestore "cosmossdk.io/core/store"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// PopulateQueuePendingSlots scans existing validator, UBD, and redelegation queue entries
// and populates the corresponding pending slot indexes used by the end-block optimization.
// Chains upgrading from a state without these indexes should call this once from their
// upgrade handler instead of relying on a consensus version migration.
func (k Keeper) PopulateQueuePendingSlots(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	if err := populateValidatorQueuePending(ctx, store, k.SetValidatorQueuePendingSlots); err != nil {
		return err
	}
	if err := populateTimeQueuePending(ctx, store, types.UnbondingQueueKey, k.SetUBDQueuePendingSlots); err != nil {
		return err
	}
	return populateTimeQueuePending(ctx, store, types.RedelegationQueueKey, k.SetRedelegationQueuePendingSlots)
}

func populateValidatorQueuePending(
	ctx context.Context,
	store corestore.KVStore,
	setter func(context.Context, []types.TimeHeightQueueSlot) error,
) error {
	iter, err := store.Iterator(types.ValidatorQueueKey, storetypes.PrefixEndBytes(types.ValidatorQueueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []types.TimeHeightQueueSlot
	for ; iter.Valid(); iter.Next() {
		keyTime, keyHeight, err := types.ParseValidatorQueueKey(iter.Key())
		if err != nil {
			return err
		}
		slots = append(slots, types.TimeHeightQueueSlot{Time: keyTime, Height: keyHeight})
	}
	return setter(ctx, slots)
}

func populateTimeQueuePending(
	ctx context.Context,
	store corestore.KVStore,
	queueKey []byte,
	setter func(context.Context, []time.Time) error,
) error {
	iter, err := store.Iterator(queueKey, storetypes.PrefixEndBytes(queueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []time.Time
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) <= len(queueKey) {
			return fmt.Errorf("key length is too short")
		}
		timeBz := key[len(queueKey):]
		t, err := sdk.ParseTimeBytes(timeBz)
		if err != nil {
			return fmt.Errorf("unable to parse time from queue key %x: %w", key, err)
		}
		slots = append(slots, t)
	}
	return setter(ctx, slots)
}

// Binary encoding constants for pending slot lists.
// Layout: [countBytes] (uint32) then for each slot: [timeBytes][heightBytes] (validator) or [timeBytes] (UBD/redelegation).
const (
	countBytes              = 4 // bytes for slot count (uint32 big-endian)
	timeSlotSizeBytes       = 8 // uint64 bytes per slot for time-only queues  (UBD, redelegation)
	heightSlotSizeBytes     = 8 // uint64 bytes for height used for unbonding validators
	timeHeightSlotSizeBytes = timeSlotSizeBytes + heightSlotSizeBytes
)

func insufficientCapacity(bz []byte, count, slotSize uint64) bool {
	actualBytes := uint64(len(bz))
	requiredBytes := count * slotSize
	return actualBytes < requiredBytes
}

// GetValidatorQueuePendingSlots reads the list of (time, height) slots that have validator queue entries.
func (k Keeper) GetValidatorQueuePendingSlots(ctx context.Context) ([]types.TimeHeightQueueSlot, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.ValidatorQueuePendingSlotsKey)
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, nil
	}
	if len(bz) < countBytes {
		return nil, fmt.Errorf("%w: key=%x", types.ErrPendingQueueSlotMissingCount, types.ValidatorQueuePendingSlotsKey)
	}
	n := binary.BigEndian.Uint32(bz[:countBytes])
	if n == 0 {
		return nil, nil
	}
	bz = bz[countBytes:]
	if insufficientCapacity(bz, uint64(n), timeHeightSlotSizeBytes) {
		return nil, fmt.Errorf("%w: key=%x, count=%d", types.ErrPendingQueueSlotInsufficientCapacity, types.ValidatorQueuePendingSlotsKey, n)
	}
	slots := make([]types.TimeHeightQueueSlot, 0, n)
	for i := uint32(0); i < n; i++ {
		offset := i * timeHeightSlotSizeBytes
		nanos := binary.BigEndian.Uint64(bz[offset : offset+timeSlotSizeBytes])
		height := int64(binary.BigEndian.Uint64(bz[offset+timeSlotSizeBytes : offset+timeHeightSlotSizeBytes]))
		slots = append(slots, types.TimeHeightQueueSlot{
			Time:   time.Unix(0, int64(nanos)).UTC(),
			Height: height,
		})
	}
	return slots, nil
}

// SetValidatorQueuePendingSlots sets the validator queue pending slots.
func (k Keeper) SetValidatorQueuePendingSlots(ctx context.Context, slots []types.TimeHeightQueueSlot) error {
	store := k.storeService.OpenKVStore(ctx)
	if len(slots) == 0 {
		return store.Delete(types.ValidatorQueuePendingSlotsKey)
	}

	sortAscending := func(i, j int) bool {
		if slots[i].Time.Before(slots[j].Time) {
			return true
		}
		if slots[j].Time.Before(slots[i].Time) {
			return false
		}
		return slots[i].Height < slots[j].Height
	}

	sort.Slice(slots, sortAscending)

	seen := make(map[string]struct{})
	uniqueSlots := make([]types.TimeHeightQueueSlot, 0, len(slots))
	for _, s := range slots {
		key := string(binary.BigEndian.AppendUint64(nil, uint64(s.Time.UnixNano()))) +
			string(binary.BigEndian.AppendUint64(nil, uint64(s.Height)))
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			uniqueSlots = append(uniqueSlots, s)
		}
	}

	bz := make([]byte, countBytes+len(uniqueSlots)*timeHeightSlotSizeBytes)
	binary.BigEndian.PutUint32(bz[:countBytes], uint32(len(uniqueSlots)))
	for i, s := range uniqueSlots {
		offset := countBytes + i*timeHeightSlotSizeBytes
		binary.BigEndian.PutUint64(bz[offset:offset+timeSlotSizeBytes], uint64(s.Time.UnixNano()))
		binary.BigEndian.PutUint64(bz[offset+timeSlotSizeBytes:offset+timeHeightSlotSizeBytes], uint64(s.Height))
	}
	return store.Set(types.ValidatorQueuePendingSlotsKey, bz)
}

// AddValidatorQueuePendingSlot adds (time, height) to the pending list if not already present.
func (k Keeper) AddValidatorQueuePendingSlot(ctx context.Context, endTime time.Time, endHeight int64) error {
	slots, err := k.GetValidatorQueuePendingSlots(ctx)
	if err != nil {
		return err
	}
	slots = append(slots, types.TimeHeightQueueSlot{Time: endTime, Height: endHeight})
	return k.SetValidatorQueuePendingSlots(ctx, slots)
}

// RemoveValidatorQueuePendingSlot removes (time, height) from the pending list.
func (k Keeper) RemoveValidatorQueuePendingSlot(ctx context.Context, endTime time.Time, endHeight int64) error {
	slots, err := k.GetValidatorQueuePendingSlots(ctx)
	if err != nil {
		return err
	}
	newSlots := make([]types.TimeHeightQueueSlot, 0, len(slots))
	for _, s := range slots {
		if toRemain := !s.Time.Equal(endTime) || s.Height != endHeight; toRemain {
			newSlots = append(newSlots, s)
		}
	}
	return k.SetValidatorQueuePendingSlots(ctx, newSlots)
}

// --- Time queue pending (time only) - shared by UBD and Redelegation ---

// getTimeQueuePendingSlots reads the list of time slots for the given key.
func (k Keeper) getTimeQueuePendingSlots(ctx context.Context, key []byte) ([]time.Time, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(key)
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, nil
	}
	if len(bz) < countBytes {
		return nil, fmt.Errorf("%w: key=%x", types.ErrPendingQueueSlotMissingCount, key)
	}
	n := binary.BigEndian.Uint32(bz[:countBytes])
	if n == 0 {
		return nil, nil
	}
	bz = bz[countBytes:]
	if insufficientCapacity(bz, uint64(n), timeSlotSizeBytes) {
		return nil, fmt.Errorf("%w: key=%x, count=%d", types.ErrPendingQueueSlotInsufficientCapacity, key, n)
	}
	slots := make([]time.Time, 0, n)
	for i := uint32(0); i < n; i++ {
		off := i * timeSlotSizeBytes
		nanos := binary.BigEndian.Uint64(bz[off : off+timeSlotSizeBytes])
		slots = append(slots, time.Unix(0, int64(nanos)).UTC())
	}
	return slots, nil
}

// setTimeQueuePendingSlots sets the time queue pending slots for the given key.
func (k Keeper) setTimeQueuePendingSlots(ctx context.Context, key []byte, slots []time.Time) error {
	store := k.storeService.OpenKVStore(ctx)
	if len(slots) == 0 {
		return store.Delete(key)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Before(slots[j]) })
	seen := make(map[int64]struct{})
	uniqueSlots := make([]time.Time, 0, len(slots))
	for _, t := range slots {
		n := t.UnixNano()
		if _, ok := seen[n]; !ok {
			seen[n] = struct{}{}
			uniqueSlots = append(uniqueSlots, t)
		}
	}
	bz := make([]byte, countBytes+len(uniqueSlots)*timeSlotSizeBytes)
	binary.BigEndian.PutUint32(bz[:countBytes], uint32(len(uniqueSlots)))
	for i, t := range uniqueSlots {
		binary.BigEndian.PutUint64(bz[countBytes+i*timeSlotSizeBytes:countBytes+(i+1)*timeSlotSizeBytes], uint64(t.UnixNano()))
	}
	return store.Set(key, bz)
}

// addTimeQueuePendingSlot adds a time slot to the pending list if not already present.
func (k Keeper) addTimeQueuePendingSlot(ctx context.Context, key []byte, completionTime time.Time) error {
	slots, err := k.getTimeQueuePendingSlots(ctx, key)
	if err != nil {
		return err
	}
	slots = append(slots, completionTime)
	return k.setTimeQueuePendingSlots(ctx, key, slots)
}

// --- UBD queue pending (time only) ---

// GetUBDQueuePendingSlots reads the list of time slots that have UBD queue entries.
func (k Keeper) GetUBDQueuePendingSlots(ctx context.Context) ([]time.Time, error) {
	return k.getTimeQueuePendingSlots(ctx, types.UBDQueuePendingSlotsKey)
}

// SetUBDQueuePendingSlots sets the UBD queue pending slots.
func (k Keeper) SetUBDQueuePendingSlots(ctx context.Context, slots []time.Time) error {
	return k.setTimeQueuePendingSlots(ctx, types.UBDQueuePendingSlotsKey, slots)
}

// AddUBDQueuePendingSlot adds a time slot to the UBD pending list if not already present.
func (k Keeper) AddUBDQueuePendingSlot(ctx context.Context, completionTime time.Time) error {
	return k.addTimeQueuePendingSlot(ctx, types.UBDQueuePendingSlotsKey, completionTime)
}

// --- Redelegation queue pending (time only) ---

// GetRedelegationQueuePendingSlots reads the list of time slots that have redelegation queue entries.
func (k Keeper) GetRedelegationQueuePendingSlots(ctx context.Context) ([]time.Time, error) {
	return k.getTimeQueuePendingSlots(ctx, types.RedelegationQueuePendingSlotsKey)
}

// SetRedelegationQueuePendingSlots sets the redelegation queue pending slots.
func (k Keeper) SetRedelegationQueuePendingSlots(ctx context.Context, slots []time.Time) error {
	return k.setTimeQueuePendingSlots(ctx, types.RedelegationQueuePendingSlotsKey, slots)
}

// AddRedelegationQueuePendingSlot adds a time slot to the redelegation pending list if not already present.
func (k Keeper) AddRedelegationQueuePendingSlot(ctx context.Context, completionTime time.Time) error {
	return k.addTimeQueuePendingSlot(ctx, types.RedelegationQueuePendingSlotsKey, completionTime)
}
