package keeper

import (
	"context"
	"encoding/binary"
	"sort"
	"time"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// validatorQueueSlot is a (time, height) slot in the validator unbonding queue.
type validatorQueueSlot struct {
	Time   time.Time
	Height int64
}

// Binary encoding constants for pending slot lists.
// Layout: [slotCountBytes] (uint32) then for each slot: [timeBytes][heightBytes] (validator) or [timeBytes] (UBD/redelegation).
const (
	pendingSlotsCountBytes   = 4 // bytes for slot count (uint32 big-endian)
	bytesPerUint64           = 8 // bytes for uint64 (time UnixNano, or height)
	validatorSlotTimeBytes   = 8 // bytes per slot for time (UnixNano) in validator queue
	validatorSlotHeightBytes = 8 // bytes per slot for height in validator queue
	validatorSlotSize        = validatorSlotTimeBytes + validatorSlotHeightBytes
	timeSlotSize             = bytesPerUint64 // bytes per slot for time-only queues (UBD, redelegation)
)

// getValidatorQueuePendingSlots reads the list of (time, height) slots that have validator queue entries.
func (k Keeper) getValidatorQueuePendingSlots(ctx context.Context) ([]validatorQueueSlot, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.ValidatorQueuePendingSlotsKey)
	if err != nil {
		return nil, err
	}
	if len(bz) < pendingSlotsCountBytes {
		return nil, nil
	}
	n := binary.BigEndian.Uint32(bz[:pendingSlotsCountBytes])
	if n == 0 {
		return nil, nil
	}
	bz = bz[pendingSlotsCountBytes:]
	if uint64(len(bz)) < uint64(n)*validatorSlotSize {
		return nil, nil
	}
	slots := make([]validatorQueueSlot, 0, n)
	for i := uint32(0); i < n; i++ {
		off := i * validatorSlotSize
		nanos := binary.BigEndian.Uint64(bz[off : off+validatorSlotTimeBytes])
		height := int64(binary.BigEndian.Uint64(bz[off+validatorSlotTimeBytes : off+validatorSlotSize]))
		slots = append(slots, validatorQueueSlot{
			Time:   time.Unix(0, int64(nanos)).UTC(),
			Height: height,
		})
	}
	return slots, nil
}

func (k Keeper) setValidatorQueuePendingSlots(ctx context.Context, slots []validatorQueueSlot) error {
	store := k.storeService.OpenKVStore(ctx)
	if len(slots) == 0 {
		return store.Delete(types.ValidatorQueuePendingSlotsKey)
	}
	// deterministic order
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Time.Before(slots[j].Time) {
			return true
		}
		if slots[j].Time.Before(slots[i].Time) {
			return false
		}
		return slots[i].Height < slots[j].Height
	})
	bz := make([]byte, pendingSlotsCountBytes+len(slots)*validatorSlotSize)
	binary.BigEndian.PutUint32(bz[:pendingSlotsCountBytes], uint32(len(slots)))
	for i, s := range slots {
		off := pendingSlotsCountBytes + i*validatorSlotSize
		binary.BigEndian.PutUint64(bz[off:off+validatorSlotTimeBytes], uint64(s.Time.UnixNano()))
		binary.BigEndian.PutUint64(bz[off+validatorSlotTimeBytes:off+validatorSlotSize], uint64(s.Height))
	}
	return store.Set(types.ValidatorQueuePendingSlotsKey, bz)
}

// addValidatorQueuePendingSlot adds (time, height) to the pending list if not already present.
func (k Keeper) addValidatorQueuePendingSlot(ctx context.Context, endTime time.Time, endHeight int64) error {
	slots, err := k.getValidatorQueuePendingSlots(ctx)
	if err != nil {
		return err
	}
	for _, s := range slots {
		if s.Time.Equal(endTime) && s.Height == endHeight {
			return nil // already present
		}
	}
	slots = append(slots, validatorQueueSlot{Time: endTime, Height: endHeight})
	return k.setValidatorQueuePendingSlots(ctx, slots)
}

// removeValidatorQueuePendingSlot removes (time, height) from the pending list.
func (k Keeper) removeValidatorQueuePendingSlot(ctx context.Context, endTime time.Time, endHeight int64) error {
	slots, err := k.getValidatorQueuePendingSlots(ctx)
	if err != nil {
		return err
	}
	newSlots := make([]validatorQueueSlot, 0, len(slots))
	for _, s := range slots {
		if !s.Time.Equal(endTime) || s.Height != endHeight {
			newSlots = append(newSlots, s)
		}
	}
	return k.setValidatorQueuePendingSlots(ctx, newSlots)
}

// --- UBD queue pending (time only) ---

func (k Keeper) getUBDQueuePendingSlots(ctx context.Context) ([]time.Time, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.UBDQueuePendingSlotsKey)
	if err != nil {
		return nil, err
	}
	if len(bz) < pendingSlotsCountBytes {
		return nil, nil
	}
	n := binary.BigEndian.Uint32(bz[:pendingSlotsCountBytes])
	if n == 0 {
		return nil, nil
	}
	bz = bz[pendingSlotsCountBytes:]
	if uint64(len(bz)) < uint64(n)*timeSlotSize {
		return nil, nil
	}
	slots := make([]time.Time, 0, n)
	for i := uint32(0); i < n; i++ {
		off := i * timeSlotSize
		nanos := binary.BigEndian.Uint64(bz[off : off+timeSlotSize])
		slots = append(slots, time.Unix(0, int64(nanos)).UTC())
	}
	return slots, nil
}

func (k Keeper) setUBDQueuePendingSlots(ctx context.Context, slots []time.Time) error {
	store := k.storeService.OpenKVStore(ctx)
	if len(slots) == 0 {
		return store.Delete(types.UBDQueuePendingSlotsKey)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Before(slots[j]) })
	seen := make(map[int64]struct{})
	deduped := make([]time.Time, 0, len(slots))
	for _, t := range slots {
		n := t.UnixNano()
		if _, ok := seen[n]; !ok {
			seen[n] = struct{}{}
			deduped = append(deduped, t)
		}
	}
	slots = deduped
	bz := make([]byte, pendingSlotsCountBytes+len(slots)*timeSlotSize)
	binary.BigEndian.PutUint32(bz[:pendingSlotsCountBytes], uint32(len(slots)))
	for i, t := range slots {
		binary.BigEndian.PutUint64(bz[pendingSlotsCountBytes+i*timeSlotSize:pendingSlotsCountBytes+(i+1)*timeSlotSize], uint64(t.UnixNano()))
	}
	return store.Set(types.UBDQueuePendingSlotsKey, bz)
}

func (k Keeper) addUBDQueuePendingSlot(ctx context.Context, completionTime time.Time) error {
	slots, err := k.getUBDQueuePendingSlots(ctx)
	if err != nil {
		return err
	}
	n := completionTime.UnixNano()
	for _, t := range slots {
		if t.UnixNano() == n {
			return nil
		}
	}
	slots = append(slots, completionTime)
	return k.setUBDQueuePendingSlots(ctx, slots)
}

// --- Redelegation queue pending (time only) ---

func (k Keeper) getRedelegationQueuePendingSlots(ctx context.Context) ([]time.Time, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.RedelegationQueuePendingSlotsKey)
	if err != nil {
		return nil, err
	}
	if len(bz) < pendingSlotsCountBytes {
		return nil, nil
	}
	n := binary.BigEndian.Uint32(bz[:pendingSlotsCountBytes])
	if n == 0 {
		return nil, nil
	}
	bz = bz[pendingSlotsCountBytes:]
	if uint64(len(bz)) < uint64(n)*timeSlotSize {
		return nil, nil
	}
	slots := make([]time.Time, 0, n)
	for i := uint32(0); i < n; i++ {
		off := i * timeSlotSize
		nanos := binary.BigEndian.Uint64(bz[off : off+timeSlotSize])
		slots = append(slots, time.Unix(0, int64(nanos)).UTC())
	}
	return slots, nil
}

func (k Keeper) setRedelegationQueuePendingSlots(ctx context.Context, slots []time.Time) error {
	store := k.storeService.OpenKVStore(ctx)
	if len(slots) == 0 {
		return store.Delete(types.RedelegationQueuePendingSlotsKey)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Before(slots[j]) })
	seen := make(map[int64]struct{})
	deduped := make([]time.Time, 0, len(slots))
	for _, t := range slots {
		n := t.UnixNano()
		if _, ok := seen[n]; !ok {
			seen[n] = struct{}{}
			deduped = append(deduped, t)
		}
	}
	slots = deduped
	bz := make([]byte, pendingSlotsCountBytes+len(slots)*timeSlotSize)
	binary.BigEndian.PutUint32(bz[:pendingSlotsCountBytes], uint32(len(slots)))
	for i, t := range slots {
		binary.BigEndian.PutUint64(bz[pendingSlotsCountBytes+i*timeSlotSize:pendingSlotsCountBytes+(i+1)*timeSlotSize], uint64(t.UnixNano()))
	}
	return store.Set(types.RedelegationQueuePendingSlotsKey, bz)
}

func (k Keeper) addRedelegationQueuePendingSlot(ctx context.Context, completionTime time.Time) error {
	slots, err := k.getRedelegationQueuePendingSlots(ctx)
	if err != nil {
		return err
	}
	n := completionTime.UnixNano()
	for _, t := range slots {
		if t.UnixNano() == n {
			return nil
		}
	}
	slots = append(slots, completionTime)
	return k.setRedelegationQueuePendingSlots(ctx, slots)
}

// populateValidatorQueuePendingFromIterator is used only by Migrate5to6 to seed the
// pending index from current queue state. End-block does not use the iterator.
func (k Keeper) populateValidatorQueuePendingFromIterator(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	iter, err := store.Iterator(types.ValidatorQueueKey, storetypes.PrefixEndBytes(types.ValidatorQueueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []validatorQueueSlot
	for ; iter.Valid(); iter.Next() {
		keyTime, keyHeight, err := types.ParseValidatorQueueKey(iter.Key())
		if err != nil {
			return err
		}
		slots = append(slots, validatorQueueSlot{Time: keyTime, Height: keyHeight})
	}
	return k.setValidatorQueuePendingSlots(ctx, slots)
}

// populateUBDQueuePendingFromIterator is used only by Migrate5to6. End-block does not use the iterator.
func (k Keeper) populateUBDQueuePendingFromIterator(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	iter, err := store.Iterator(types.UnbondingQueueKey, storetypes.PrefixEndBytes(types.UnbondingQueueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []time.Time
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) <= len(types.UnbondingQueueKey) {
			continue
		}
		timeBz := key[len(types.UnbondingQueueKey):]
		t, parseErr := sdk.ParseTimeBytes(timeBz)
		if parseErr != nil {
			continue
		}
		slots = append(slots, t)
	}
	return k.setUBDQueuePendingSlots(ctx, slots)
}

// populateRedelegationQueuePendingFromIterator is used only by Migrate5to6. End-block does not use the iterator.
func (k Keeper) populateRedelegationQueuePendingFromIterator(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	iter, err := store.Iterator(types.RedelegationQueueKey, storetypes.PrefixEndBytes(types.RedelegationQueueKey))
	if err != nil {
		return err
	}
	defer iter.Close()
	var slots []time.Time
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) <= len(types.RedelegationQueueKey) {
			continue
		}
		timeBz := key[len(types.RedelegationQueueKey):]
		t, err := sdk.ParseTimeBytes(timeBz)
		if err != nil {
			continue
		}
		slots = append(slots, t)
	}
	return k.setRedelegationQueuePendingSlots(ctx, slots)
}
