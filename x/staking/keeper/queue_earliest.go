package keeper

import (
	"bytes"
	"context"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// queueEmptyMarker records that a queue is known to be empty. An absent key means the earliest
// time is not known yet, so end-block scans the queue once and records it.
var queueEmptyMarker = []byte{0}

// queueGate pairs a maturity queue with the key holding its earliest entry time.
//
// Invariant: a stored time is never later than any entry in the queue, and the empty marker
// is stored only when the queue has no entries. Inserts keep it by lowering the time; deletes
// cannot break it; end-block scans recompute it exactly.
type queueGate struct {
	earliestKey []byte
	queuePrefix []byte
	keyTime     func(key []byte) (time.Time, error)
}

var (
	validatorQueueGate = queueGate{
		earliestKey: types.ValidatorQueueEarliestKey,
		queuePrefix: types.ValidatorQueueKey,
		keyTime: func(key []byte) (time.Time, error) {
			t, _, err := types.ParseValidatorQueueKey(key)
			return t, err
		},
	}
	ubdQueueGate = queueGate{
		earliestKey: types.UBDQueueEarliestKey,
		queuePrefix: types.UnbondingQueueKey,
		keyTime:     timeQueueKeyTime(types.UnbondingQueueKey),
	}
	redelegationQueueGate = queueGate{
		earliestKey: types.RedelegationQueueEarliestKey,
		queuePrefix: types.RedelegationQueueKey,
		keyTime:     timeQueueKeyTime(types.RedelegationQueueKey),
	}
)

func timeQueueKeyTime(prefix []byte) func([]byte) (time.Time, error) {
	return func(key []byte) (time.Time, error) {
		return sdk.ParseTimeBytes(key[len(prefix):])
	}
}

// PopulateQueuePendingSlots recomputes the earliest entry time of the validator, unbonding and
// redelegation queues. It also replaces the pending-slot lists that fork v0.54.x stored under
// the same keys.
// End-block recomputes a missing or legacy time by itself, so upgrade handlers don't need to call
// this; it is kept for handlers written against fork v0.54.x.
func (k Keeper) PopulateQueuePendingSlots(ctx context.Context) error {
	for _, g := range []queueGate{validatorQueueGate, ubdQueueGate, redelegationQueueGate} {
		if err := k.refreshQueueEarliest(ctx, g); err != nil {
			return err
		}
	}
	return nil
}

type queueEarliest struct {
	known bool // false when absent or still in the fork v0.54.x pending-slot list format
	empty bool
	time  time.Time
}

func (k Keeper) getQueueEarliest(ctx context.Context, g queueGate) (queueEarliest, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(g.earliestKey)
	if err != nil || len(bz) == 0 {
		return queueEarliest{}, err
	}
	if bytes.Equal(bz, queueEmptyMarker) {
		return queueEarliest{known: true, empty: true}, nil
	}
	t, err := sdk.ParseTimeBytes(bz)
	if err != nil {
		// A fork v0.54.x pending-slot list never parses as a time; the next scan replaces it.
		return queueEarliest{}, nil
	}
	return queueEarliest{known: true, time: t}, nil
}

// queueDue reports whether end-block has to scan the queue at now.
func (k Keeper) queueDue(ctx context.Context, g queueGate, now time.Time) (bool, error) {
	e, err := k.getQueueEarliest(ctx, g)
	if err != nil {
		return false, err
	}
	if !e.known {
		return true, nil
	}
	return !e.empty && !e.time.After(now), nil
}

// lowerQueueEarliest records t as the earliest entry time if it is earlier than the stored one.
// It runs on every queue insert, so it reads one fixed-size value and writes it only when lowering.
func (k Keeper) lowerQueueEarliest(ctx context.Context, g queueGate, t time.Time) error {
	e, err := k.getQueueEarliest(ctx, g)
	if err != nil {
		return err
	}
	// An unknown time stays unknown: the queue may hold earlier entries that only a scan finds.
	if !e.known || (!e.empty && !t.Before(e.time)) {
		return nil
	}
	return k.storeService.OpenKVStore(ctx).Set(g.earliestKey, sdk.FormatTimeBytes(t))
}

// refreshQueueEarliest records the time of the queue's first entry, or the empty marker.
func (k Keeper) refreshQueueEarliest(ctx context.Context, g queueGate) error {
	value, err := k.firstQueueEntryTime(ctx, g)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	prev, err := store.Get(g.earliestKey)
	if err != nil || bytes.Equal(prev, value) {
		return err
	}
	return store.Set(g.earliestKey, value)
}

func (k Keeper) firstQueueEntryTime(ctx context.Context, g queueGate) ([]byte, error) {
	iter, err := k.storeService.OpenKVStore(ctx).Iterator(g.queuePrefix, storetypes.PrefixEndBytes(g.queuePrefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if !iter.Valid() {
		return queueEmptyMarker, nil
	}
	t, err := g.keyTime(iter.Key())
	if err != nil {
		return nil, err
	}
	return sdk.FormatTimeBytes(t), nil
}
