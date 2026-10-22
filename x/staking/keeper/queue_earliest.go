package keeper

import (
	"bytes"
	"context"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// queueEmptyMarker is the stored value for a queue known to be empty. An absent key means the
// earliest time is unknown, so end-block scans the queue from its start and records it.
const queueEmptyMarker = "\x00"

// sortableTimeLen is the width of sdk.FormatTimeBytes for years 0000-9999, the only times stored:
// at this width byte order is time order and queue-key order. Other years leave the time unknown.
const sortableTimeLen = len(sdk.SortableTimeFormat)

// queueGate pairs a maturity queue with the key holding its earliest entry time.
//
// Invariant: the first queue key at the stored time sorts at or before every key in the queue, and
// the empty marker is stored only when the queue has no entries. Inserts keep it by lowering the
// time; deletes cannot break it; end-block scans start at the stored time and recompute it from
// the first remaining entry.
type queueGate struct {
	earliestKey []byte
	queuePrefix []byte
	// keyTimeBytes returns the formatted time inside a queue key.
	keyTimeBytes func(key []byte) []byte
	// firstKeyAt returns the lowest queue key with the given formatted time.
	firstKeyAt func(timeBz []byte) []byte
}

// newTimeQueueGate gates a queue whose keys are its prefix followed by the formatted time.
func newTimeQueueGate(earliestKey, queuePrefix []byte) queueGate {
	return queueGate{
		earliestKey:  earliestKey,
		queuePrefix:  queuePrefix,
		keyTimeBytes: func(key []byte) []byte { return key[len(queuePrefix):] },
		firstKeyAt:   func(timeBz []byte) []byte { return append(bytes.Clone(queuePrefix), timeBz...) },
	}
}

var (
	validatorQueueGate = queueGate{
		earliestKey:  types.ValidatorQueueEarliestKey,
		queuePrefix:  types.ValidatorQueueKey,
		keyTimeBytes: types.ValidatorQueueKeyTimeBytes,
		firstKeyAt:   validatorQueueFirstKeyAt,
	}
	ubdQueueGate          = newTimeQueueGate(types.UBDQueueEarliestKey, types.UnbondingQueueKey)
	redelegationQueueGate = newTimeQueueGate(types.RedelegationQueueEarliestKey, types.RedelegationQueueKey)

	queueGates = []queueGate{validatorQueueGate, ubdQueueGate, redelegationQueueGate}
)

// validatorQueueFirstKeyAt uses height 0, the lowest a key can hold; a zero-height genesis export
// writes entries at height 0. Bytes that don't parse as a time fall back to the queue start.
func validatorQueueFirstKeyAt(timeBz []byte) []byte {
	t, err := sdk.ParseTimeBytes(timeBz)
	if err != nil {
		return types.ValidatorQueueKey
	}
	return types.GetValidatorQueueKey(t, 0)
}

// RecomputeQueueEarliest recomputes the earliest entry time of the validator, unbonding and
// redelegation queues from each queue's first entry, replacing any pending-slot list that fork
// v0.54.x stored under the same keys. Staking's 5 to 6 migration runs it; run it again after
// writing queue entries without the keeper's queue setters.
func (k Keeper) RecomputeQueueEarliest(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	for _, g := range queueGates {
		prev, err := store.Get(g.earliestKey)
		if err != nil {
			return err
		}
		next, err := k.earliestFrom(ctx, g, g.queuePrefix)
		if err != nil {
			return err
		}
		if err := k.storeQueueEarliest(ctx, g, prev, next); err != nil {
			return err
		}
	}
	return nil
}

// PopulateQueuePendingSlots is kept for upgrade handlers written against fork v0.54.x.
//
// Deprecated: use RecomputeQueueEarliest.
func (k Keeper) PopulateQueuePendingSlots(ctx context.Context) error {
	return k.RecomputeQueueEarliest(ctx)
}

// runGatedScan runs scan once g's earliest time is due at nowBz, the formatted block time. scan
// returns the value stored when it finishes and the earliest time to store, which is written if it
// differs.
func (k Keeper) runGatedScan(ctx context.Context, g queueGate, nowBz []byte, scan func(earliest queueEarliest) (stored, next []byte, err error)) error {
	earliest, err := k.getQueueEarliest(ctx, g)
	if err != nil || !earliest.due(nowBz) {
		return err
	}
	stored, next, err := scan(earliest)
	if err != nil {
		return err
	}
	return k.storeQueueEarliest(ctx, g, stored, next)
}

// dequeueTimeQueue removes every timeslice keyed at or before now and returns their entries, once
// the queue's earliest time is due. The scan's iterator runs past now and stops at the first later
// key, which becomes the new earliest time, so finding it costs no second walk and cannot
// disagree with what the scan removed.
func dequeueTimeQueue[E any](ctx context.Context, k Keeper, g queueGate, now time.Time, unmarshal func([]byte) ([]E, error)) ([]E, error) {
	var mature []E
	nowBz := sdk.FormatTimeBytes(now)
	err := k.runGatedScan(ctx, g, nowBz, func(earliest queueEarliest) (stored, next []byte, err error) {
		mature, next, err = dequeueTimeSlices(ctx, k, g, earliest.scanStart(g), nowBz, unmarshal)
		return earliest.raw, next, err
	})
	if err != nil {
		return nil, err
	}
	return mature, nil
}

func dequeueTimeSlices[E any](ctx context.Context, k Keeper, g queueGate, start, nowBz []byte, unmarshal func([]byte) ([]E, error)) (mature []E, next []byte, err error) {
	store := k.storeService.OpenKVStore(ctx)
	bound := g.firstKeyAt(nowBz)
	iter, err := store.Iterator(start, storetypes.PrefixEndBytes(g.queuePrefix))
	if err != nil {
		return nil, nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if bytes.Compare(key, bound) > 0 {
			return mature, g.earliestValue(key), nil
		}
		entries, err := unmarshal(iter.Value())
		if err != nil {
			return nil, nil, err
		}
		mature = append(mature, entries...)
		if err = store.Delete(key); err != nil {
			return nil, nil, err
		}
	}
	return mature, []byte(queueEmptyMarker), nil
}

type queueEarliest struct {
	raw   []byte
	known bool // false when the stored value is neither a time nor the empty marker
	empty bool
}

// due reports whether end-block has to scan the queue at nowBz, the formatted block time.
func (e queueEarliest) due(nowBz []byte) bool {
	return !e.known || (!e.empty && bytes.Compare(e.raw, nowBz) <= 0)
}

// scanStart returns the first key at the stored time, which sorts at or before every queue key, or
// the queue start when that time is unknown.
func (e queueEarliest) scanStart(g queueGate) []byte {
	if !e.known {
		return g.queuePrefix
	}
	return g.firstKeyAt(e.raw)
}

func (k Keeper) getQueueEarliest(ctx context.Context, g queueGate) (queueEarliest, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(g.earliestKey)
	switch {
	case err != nil:
		return queueEarliest{}, err
	case string(bz) == queueEmptyMarker:
		return queueEarliest{raw: bz, known: true, empty: true}, nil
	case len(bz) == sortableTimeLen:
		return queueEarliest{raw: bz, known: true}, nil
	default:
		// Absent, or a fork v0.54.x pending-slot list (4+8n or 4+16n bytes, never 1 or 29); the
		// next scan replaces it.
		return queueEarliest{raw: bz}, nil
	}
}

// earliestValue is the stored value for a queue whose first entry has key, or nil (unknown) when
// that entry's time isn't sortableTimeLen wide.
func (g queueGate) earliestValue(key []byte) []byte {
	bz := g.keyTimeBytes(key)
	if len(bz) != sortableTimeLen {
		return nil
	}
	return bytes.Clone(bz)
}

// lowerQueueEarliest records t as the earliest entry time if it is earlier than the stored one, or
// clears a known time when t isn't sortableTimeLen wide. It runs on every queue insert: it reads
// the stored value, 0, 1 or 29 bytes once any fork v0.54.x list is replaced, and writes only on a
// change.
func (k Keeper) lowerQueueEarliest(ctx context.Context, g queueGate, t time.Time) error {
	e, err := k.getQueueEarliest(ctx, g)
	// An unknown time stays unknown: the queue may hold earlier entries that only a scan finds.
	if err != nil || !e.known {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	bz := sdk.FormatTimeBytes(t)
	if len(bz) != sortableTimeLen {
		return store.Delete(g.earliestKey)
	}
	if !e.empty && bytes.Compare(bz, e.raw) >= 0 {
		return nil
	}
	return store.Set(g.earliestKey, bz)
}

// earliestFrom returns the value to store for the queue's first entry at or after start: its time,
// the empty marker when there is none, or nil when that time isn't sortableTimeLen wide.
func (k Keeper) earliestFrom(ctx context.Context, g queueGate, start []byte) ([]byte, error) {
	iter, err := k.storeService.OpenKVStore(ctx).Iterator(start, storetypes.PrefixEndBytes(g.queuePrefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if !iter.Valid() {
		return []byte(queueEmptyMarker), nil
	}
	return g.earliestValue(iter.Key()), nil
}

// storeQueueEarliest writes value, or clears the key when value is nil, unless it equals prev, the
// value currently stored, so a queue that stays due is not rewritten every block.
func (k Keeper) storeQueueEarliest(ctx context.Context, g queueGate, prev, value []byte) error {
	if bytes.Equal(prev, value) {
		return nil
	}
	store := k.storeService.OpenKVStore(ctx)
	if value == nil {
		return store.Delete(g.earliestKey)
	}
	return store.Set(g.earliestKey, value)
}
