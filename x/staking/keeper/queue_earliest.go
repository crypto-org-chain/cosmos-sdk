package keeper

import (
	"bytes"
	"context"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// queueEmptyMarker is stored for a queue known to be empty; an absent key means unknown.
const queueEmptyMarker = "\x00"

// sortableTimeLen is the width of sdk.FormatTimeBytes for years 0000-9999, where byte order is time
// and queue-key order. Only times of this width are stored.
const sortableTimeLen = len(sdk.SortableTimeFormat)

// queueGate pairs a maturity queue with the key holding its earliest entry time.
//
// Invariant: the first queue key at the stored time sorts at or before every key in the queue, and
// the empty marker is stored only for an empty queue.
type queueGate struct {
	earliestKey  []byte
	queuePrefix  []byte
	keyTimeBytes func(key []byte) []byte
	// firstKeyAt returns a key at or before every queue key with the given formatted time, and
	// after every key with an earlier time of the same width.
	firstKeyAt func(timeBz []byte) []byte
}

// newTimeQueueGate gates a queue whose keys are its prefix followed by the formatted time.
func newTimeQueueGate(earliestKey, queuePrefix []byte) queueGate {
	return queueGate{
		earliestKey:  earliestKey,
		queuePrefix:  queuePrefix,
		keyTimeBytes: func(key []byte) []byte { return key[len(queuePrefix):len(key):len(key)] },
		firstKeyAt: func(timeBz []byte) []byte {
			return append(append(make([]byte, 0, len(queuePrefix)+len(timeBz)), queuePrefix...), timeBz...)
		},
	}
}

// storableTime returns the time inside key, sharing its memory, or nil if it can't be stored.
func (g queueGate) storableTime(key []byte) []byte {
	if bz := g.keyTimeBytes(key); len(bz) == sortableTimeLen {
		return bz
	}
	return nil
}

// earliestValue is the value to store when key is the queue's first entry; nil means unknown.
func (g queueGate) earliestValue(key []byte) []byte {
	return bytes.Clone(g.storableTime(key))
}

var (
	validatorQueueGate = queueGate{
		earliestKey:  types.ValidatorQueueEarliestKey,
		queuePrefix:  types.ValidatorQueueKey,
		keyTimeBytes: types.ValidatorQueueKeyTimeBytes,
		firstKeyAt:   types.ValidatorQueueTimePrefix,
	}
	ubdQueueGate          = newTimeQueueGate(types.UBDQueueEarliestKey, types.UnbondingQueueKey)
	redelegationQueueGate = newTimeQueueGate(types.RedelegationQueueEarliestKey, types.RedelegationQueueKey)

	queueGates = []queueGate{validatorQueueGate, ubdQueueGate, redelegationQueueGate}
)

type queueEarliest struct {
	raw   []byte
	known bool // false when the stored value is neither a time nor the empty marker
	empty bool
}

// due reports whether the queue must be scanned at the formatted cutoff nowBz. A cutoff of another
// width doesn't compare by time with the stored value, so it always scans.
func (e queueEarliest) due(nowBz []byte) bool {
	return !e.known || len(nowBz) != sortableTimeLen || (!e.empty && bytes.Compare(e.raw, nowBz) <= 0)
}

// scanStart returns the first key at the stored time, or the queue start when there is none.
func (e queueEarliest) scanStart(g queueGate) []byte {
	if !e.known || e.empty {
		return g.queuePrefix
	}
	return g.firstKeyAt(e.raw)
}

// RecomputeQueueEarliest recomputes each queue's earliest entry time from its first entry,
// replacing any pending-slot list that fork v0.53.x or v0.54.x stored under the same keys. Run it
// after writing queue entries without the keeper's queue setters.
func (k Keeper) RecomputeQueueEarliest(ctx context.Context) error {
	for _, g := range queueGates {
		prev, err := k.getQueueEarliest(ctx, g)
		if err != nil {
			return err
		}
		next, err := k.earliestFrom(ctx, g, g.queuePrefix)
		if err != nil {
			return err
		}
		if err := k.storeQueueEarliest(ctx, g, prev.raw, next); err != nil {
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

// runGatedScan runs scan once g's earliest time is due at the formatted cutoff nowBz. scan returns
// the value stored when it finishes and the new earliest time, which is written if it differs.
func (k Keeper) runGatedScan(ctx context.Context, g queueGate, nowBz []byte, scan func(earliest queueEarliest) (stored, next []byte, err error)) error {
	earliest, err := k.getQueueEarliest(ctx, g)
	if err != nil {
		return err
	}
	if !earliest.due(nowBz) {
		return nil
	}
	stored, next, err := scan(earliest)
	if err != nil {
		return err
	}
	return k.storeQueueEarliest(ctx, g, stored, next)
}

// dequeueTimeQueue removes and returns every timeslice keyed at or before now, once the queue is
// due. The iterator stops at the first later key, which becomes the new earliest time. g must come
// from newTimeQueueGate, whose keys end at the time, and unmarshal must not write to the store.
func dequeueTimeQueue[E any](ctx context.Context, k Keeper, g queueGate, now time.Time, unmarshal func([]byte) ([]E, error)) ([]E, error) {
	var mature []E
	nowBz := sdk.FormatTimeBytes(now)
	err := k.runGatedScan(ctx, g, nowBz, func(earliest queueEarliest) ([]byte, []byte, error) {
		store := k.storeService.OpenKVStore(ctx)
		bound := g.firstKeyAt(nowBz)
		iter, err := store.Iterator(earliest.scanStart(g), storetypes.PrefixEndBytes(g.queuePrefix))
		if err != nil {
			return nil, nil, err
		}
		defer iter.Close()

		for ; iter.Valid(); iter.Next() {
			key := iter.Key()
			if bytes.Compare(key, bound) > 0 {
				return earliest.raw, g.earliestValue(key), nil
			}
			entries, err := unmarshal(iter.Value())
			if err != nil {
				return nil, nil, err
			}
			mature = append(mature, entries...)
			if err := store.Delete(key); err != nil {
				return nil, nil, err
			}
		}
		return earliest.raw, []byte(queueEmptyMarker), nil
	})
	if err != nil {
		return nil, err
	}
	return mature, nil
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
		// Absent, or a fork v0.53.x/v0.54.x pending-slot list (4+8n or 4+16n bytes, never 1 or 29).
		// Fork v0.53.x is already at staking version 6 and skips the migration, so a scan replaces it.
		return queueEarliest{raw: bz}, nil
	}
}

// setQueueEntry writes a queue entry and lowers the stored earliest time to the time in key, or
// clears it when that time can't be stored. Every queue insert must go through it.
func (k Keeper) setQueueEntry(ctx context.Context, g queueGate, key, value []byte) error {
	store := k.storeService.OpenKVStore(ctx)
	if err := store.Set(key, value); err != nil {
		return err
	}
	e, err := k.getQueueEarliest(ctx, g)
	if err != nil {
		return err
	}
	// An unknown time stays unknown: the queue may hold earlier entries that only a scan finds.
	if !e.known {
		return nil
	}
	timeBz := g.storableTime(key)
	if timeBz == nil {
		return store.Delete(g.earliestKey)
	}
	if !e.empty && bytes.Compare(timeBz, e.raw) >= 0 {
		return nil
	}
	return store.Set(g.earliestKey, bytes.Clone(timeBz))
}

// earliestFrom returns the value to store for the queue's first entry at or after start.
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

// storeQueueEarliest writes value (nil clears the key) unless it equals prev, the stored value, so
// a queue that stays due is not rewritten every block.
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
