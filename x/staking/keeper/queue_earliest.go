package keeper

import (
	"bytes"
	"context"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

var (
	// queueEmptyMarker records that a queue is known to be empty. An absent key means the earliest
	// time is not known, so end-block scans the queue and records it as soon as it can. Stores
	// keep the slice they are given, so values to store are copies made by emptyQueueValue.
	queueEmptyMarker = []byte{0}

	// sortableTimeLen is the length of sdk.FormatTimeBytes for years 0000-9999. Stored times are
	// compared as bytes, which for this fixed width is time order and the order of every queue's
	// keys. Other years can't reach the queues through txs or genesis (protobuf timestamps stop at
	// 9999), only through direct Set* calls; they encode longer, and the validator key's length
	// prefix sorts them apart, so they leave the stored time unknown and end-block falls back to
	// upstream's scan.
	sortableTimeLen = len(sdk.FormatTimeBytes(time.Time{}))
)

// queueGate pairs a maturity queue with the key holding its earliest entry time.
//
// Invariant: the queue key for the stored time sorts at or before every key in the queue, and the
// empty marker is stored only when the queue has no entries. Inserts keep it by lowering the
// time; deletes cannot break it; end-block scans recompute it from the first remaining entry.
type queueGate struct {
	earliestKey []byte
	queuePrefix []byte
	// keyTimeBytes returns the formatted time inside a queue key.
	keyTimeBytes func(key []byte) []byte
}

// timeQueueGate is a queue keyed by completion time alone, which end-block drains up to now.
type timeQueueGate struct {
	queueGate
	timeKey func(time.Time) []byte
}

var (
	validatorQueueGate = queueGate{
		earliestKey:  types.ValidatorQueueEarliestKey,
		queuePrefix:  types.ValidatorQueueKey,
		keyTimeBytes: types.ValidatorQueueKeyTimeBytes,
	}
	ubdQueueGate = timeQueueGate{
		queueGate: queueGate{
			earliestKey:  types.UBDQueueEarliestKey,
			queuePrefix:  types.UnbondingQueueKey,
			keyTimeBytes: func(key []byte) []byte { return key[len(types.UnbondingQueueKey):] },
		},
		timeKey: types.GetUnbondingDelegationTimeKey,
	}
	redelegationQueueGate = timeQueueGate{
		queueGate: queueGate{
			earliestKey:  types.RedelegationQueueEarliestKey,
			queuePrefix:  types.RedelegationQueueKey,
			keyTimeBytes: func(key []byte) []byte { return key[len(types.RedelegationQueueKey):] },
		},
		timeKey: types.GetRedelegationTimeKey,
	}
)

// RecomputeQueueEarliest recomputes the earliest entry time of the validator, unbonding and
// redelegation queues, replacing any pending-slot list that fork v0.54.x stored under the same
// keys. End-block recomputes a missing or legacy value by itself; chains upgrading from fork
// v0.54.x should still call this in their upgrade handler, so txs in the upgrade block don't read
// the legacy list.
func (k Keeper) RecomputeQueueEarliest(ctx context.Context) error {
	store := k.storeService.OpenKVStore(ctx)
	for _, g := range []queueGate{validatorQueueGate, ubdQueueGate.queueGate, redelegationQueueGate.queueGate} {
		prev, err := store.Get(g.earliestKey)
		if err != nil {
			return err
		}
		next, err := k.firstQueueEntryTime(ctx, g)
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

// dequeueTimeQueue removes every timeslice keyed at or before now and returns their entries, but
// only once the queue's earliest time is due. The scan's iterator runs past now and stops at the
// first later key, which becomes the new earliest time, so finding it costs no second walk and
// cannot disagree with what the scan removed.
func dequeueTimeQueue[E any](ctx context.Context, k Keeper, g timeQueueGate, now time.Time, unmarshal func([]byte) ([]E, error)) ([]E, error) {
	earliest, err := k.getQueueEarliest(ctx, g.queueGate)
	if err != nil || !earliest.due(now) {
		return nil, err
	}
	mature, next, err := dequeueTimeSlices(ctx, k, g, now, unmarshal)
	if err != nil {
		return nil, err
	}
	return mature, k.storeQueueEarliest(ctx, g.queueGate, earliest.raw, next)
}

func dequeueTimeSlices[E any](ctx context.Context, k Keeper, g timeQueueGate, now time.Time, unmarshal func([]byte) ([]E, error)) (mature []E, next []byte, err error) {
	store := k.storeService.OpenKVStore(ctx)
	bound := g.timeKey(now)
	iter, err := store.Iterator(g.queuePrefix, storetypes.PrefixEndBytes(g.queuePrefix))
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
	return mature, emptyQueueValue(), nil
}

type queueEarliest struct {
	raw   []byte
	known bool // false when absent or in the fork v0.54.x pending-slot list format
	empty bool
}

// due reports whether end-block has to scan the queue at now.
func (e queueEarliest) due(now time.Time) bool {
	return !e.known || (!e.empty && bytes.Compare(e.raw, sdk.FormatTimeBytes(now)) <= 0)
}

func (k Keeper) getQueueEarliest(ctx context.Context, g queueGate) (queueEarliest, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(g.earliestKey)
	switch {
	case err != nil:
		return queueEarliest{}, err
	case bytes.Equal(bz, queueEmptyMarker):
		return queueEarliest{raw: bz, known: true, empty: true}, nil
	case len(bz) == sortableTimeLen:
		return queueEarliest{raw: bz, known: true}, nil
	default:
		// Absent, or a fork v0.54.x pending-slot list (4+8n or 4+16n bytes, never 29); the next
		// scan replaces it.
		return queueEarliest{raw: bz}, nil
	}
}

// encodeQueueTime formats t, or returns false when it isn't the fixed width stored times use, see
// sortableTimeLen.
func encodeQueueTime(t time.Time) ([]byte, bool) {
	bz := sdk.FormatTimeBytes(t)
	return bz, len(bz) == sortableTimeLen
}

// earliestValue is the stored value for a queue whose first entry has key, or nil (unknown) when
// that entry's time isn't the fixed width stored times use.
func (g queueGate) earliestValue(key []byte) []byte {
	bz := g.keyTimeBytes(key)
	if len(bz) != sortableTimeLen {
		return nil
	}
	return bytes.Clone(bz)
}

// lowerQueueEarliest records t as the earliest entry time if it is earlier than the stored one, or
// clears a known time when t can't be stored. It runs on every queue insert: it reads the stored
// value, a fixed 1 or 29 bytes once any fork v0.54.x list is replaced, and writes only on a change.
func (k Keeper) lowerQueueEarliest(ctx context.Context, g queueGate, t time.Time) error {
	e, err := k.getQueueEarliest(ctx, g)
	// An unknown time stays unknown: the queue may hold earlier entries that only a scan finds.
	if err != nil || !e.known {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	bz, ok := encodeQueueTime(t)
	if !ok {
		return store.Delete(g.earliestKey)
	}
	if !e.empty && bytes.Compare(bz, e.raw) >= 0 {
		return nil
	}
	return store.Set(g.earliestKey, bz)
}

// firstQueueEntryTime returns the stored value for the queue's current first entry: its time, the
// empty marker, or nil when that time can't be stored.
func (k Keeper) firstQueueEntryTime(ctx context.Context, g queueGate) ([]byte, error) {
	iter, err := k.storeService.OpenKVStore(ctx).Iterator(g.queuePrefix, storetypes.PrefixEndBytes(g.queuePrefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if !iter.Valid() {
		return emptyQueueValue(), nil
	}
	return g.earliestValue(iter.Key()), nil
}

func emptyQueueValue() []byte {
	return bytes.Clone(queueEmptyMarker)
}

// storeQueueEarliest writes value (nil clears it) unless it equals prev, the value read before the
// scan, so a queue that stays due does not rewrite it every block. A scan can only lower or clear
// the stored value (the validator scan re-sets entries through lowerQueueEarliest), and either
// stays a valid bound, so a write skipped because value equals prev was never needed.
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
