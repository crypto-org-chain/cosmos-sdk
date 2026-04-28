package mempool_test

import (
	"math/rand"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

type laneSigner struct {
	address sdk.AccAddress
	nonce   uint64
}

type multiLaneSignerAdapter struct {
	lanesByID map[int][]laneSigner
}

func (a multiLaneSignerAdapter) GetSigners(tx sdk.Tx) ([]mempool.SignerData, error) {
	ttx, ok := tx.(testTx)
	if !ok {
		return mempool.NewDefaultSignerExtractionAdapter().GetSigners(tx)
	}

	lanes, ok := a.lanesByID[ttx.id]
	if !ok {
		return mempool.NewDefaultSignerExtractionAdapter().GetSigners(tx)
	}

	signers := make([]mempool.SignerData, 0, len(lanes))
	for _, lane := range lanes {
		signers = append(signers, mempool.NewSignerData(lane.address, lane.nonce))
	}
	return signers, nil
}

func TestMultiLanePriorityNonceMempool_InsertIndexesEveryLane(t *testing.T) {
	accounts := simtypes.RandomAccounts(rand.New(rand.NewSource(0)), 3)
	sa := accounts[0].Address
	sb := accounts[1].Address
	sc := accounts[2].Address
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	tx := testTx{id: 100, priority: 50, nonce: 1, address: sa}
	pool := mempool.NewMultiLanePriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
		TxPriority: mempool.NewDefaultTxPriority(),
		SignerExtractor: multiLaneSignerAdapter{
			lanesByID: map[int][]laneSigner{
				tx.id: {
					{address: sa, nonce: 1},
					{address: sb, nonce: 7},
					{address: sc, nonce: 3},
				},
			},
		},
	})

	require.NoError(t, pool.Insert(ctx.WithPriority(tx.priority), tx))
	require.Equal(t, 1, pool.CountTx())
	require.Equal(t, tx, pool.NextSenderTx(sa.String()))
	require.Equal(t, tx, pool.NextSenderTx(sb.String()))
	require.Equal(t, tx, pool.NextSenderTx(sc.String()))
}

func TestMultiLanePriorityNonceMempool_ReplacementAcrossLanes(t *testing.T) {
	accounts := simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 2)
	attacker := accounts[0].Address
	victim := accounts[1].Address
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	batchTx := testTx{id: 200, priority: 10, nonce: 1, address: attacker}
	cancelTx := testTx{id: 201, priority: 11, nonce: 3, address: victim}
	pool := mempool.NewMultiLanePriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
		TxPriority: mempool.NewDefaultTxPriority(),
		SignerExtractor: multiLaneSignerAdapter{
			lanesByID: map[int][]laneSigner{
				batchTx.id: {
					{address: attacker, nonce: 1},
					{address: victim, nonce: 3},
				},
				cancelTx.id: {
					{address: victim, nonce: 3},
				},
			},
		},
	})

	require.NoError(t, pool.Insert(ctx.WithPriority(batchTx.priority), batchTx))
	require.NoError(t, pool.Insert(ctx.WithPriority(cancelTx.priority), cancelTx))

	require.Equal(t, 1, pool.CountTx())
	require.Nil(t, pool.NextSenderTx(attacker.String()))
	require.Equal(t, cancelTx, pool.NextSenderTx(victim.String()))
}

func TestMultiLanePriorityNonceMempool_ReplacementRejectsIfAnyLaneFails(t *testing.T) {
	accounts := simtypes.RandomAccounts(rand.New(rand.NewSource(2)), 4)
	a := accounts[0].Address
	b := accounts[1].Address
	c := accounts[2].Address
	d := accounts[3].Address
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	txA := testTx{id: 300, priority: 100, nonce: 1, address: a}
	txB := testTx{id: 301, priority: 1, nonce: 1, address: b}
	candidate := testTx{id: 302, priority: 50, nonce: 1, address: c}
	pool := mempool.NewMultiLanePriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
		TxPriority: mempool.NewDefaultTxPriority(),
		TxReplacement: func(op, np int64, _, _ sdk.Tx) bool {
			return np > op
		},
		SignerExtractor: multiLaneSignerAdapter{
			lanesByID: map[int][]laneSigner{
				txA.id: {
					{address: a, nonce: 1},
				},
				txB.id: {
					{address: b, nonce: 1},
				},
				candidate.id: {
					{address: a, nonce: 1},
					{address: b, nonce: 1},
					{address: d, nonce: 9},
				},
			},
		},
	})

	require.NoError(t, pool.Insert(ctx.WithPriority(txA.priority), txA))
	require.NoError(t, pool.Insert(ctx.WithPriority(txB.priority), txB))
	err := pool.Insert(ctx.WithPriority(candidate.priority), candidate)
	require.Error(t, err)

	require.Equal(t, 2, pool.CountTx())
	require.Equal(t, txA, pool.NextSenderTx(a.String()))
	require.Equal(t, txB, pool.NextSenderTx(b.String()))
}

func TestMultiLanePriorityNonceMempool_RemoveCleansAllLanes(t *testing.T) {
	accounts := simtypes.RandomAccounts(rand.New(rand.NewSource(3)), 2)
	sa := accounts[0].Address
	sb := accounts[1].Address
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	tx := testTx{id: 400, priority: 22, nonce: 1, address: sa}
	pool := mempool.NewMultiLanePriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
		TxPriority: mempool.NewDefaultTxPriority(),
		SignerExtractor: multiLaneSignerAdapter{
			lanesByID: map[int][]laneSigner{
				tx.id: {
					{address: sa, nonce: 1},
					{address: sb, nonce: 5},
				},
			},
		},
	})

	require.NoError(t, pool.Insert(ctx.WithPriority(tx.priority), tx))
	require.NoError(t, pool.Remove(tx))
	require.Equal(t, 0, pool.CountTx())
	require.Nil(t, pool.NextSenderTx(sa.String()))
	require.Nil(t, pool.NextSenderTx(sb.String()))
	require.NoError(t, mempool.IsMultiLaneEmpty[int64](pool))
}

func TestMultiLanePriorityNonceMempool_DuplicateLaneInTxRejected(t *testing.T) {
	accounts := simtypes.RandomAccounts(rand.New(rand.NewSource(4)), 1)
	sa := accounts[0].Address
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	tx := testTx{id: 500, priority: 5, nonce: 1, address: sa}
	pool := mempool.NewMultiLanePriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
		TxPriority: mempool.NewDefaultTxPriority(),
		SignerExtractor: multiLaneSignerAdapter{
			lanesByID: map[int][]laneSigner{
				tx.id: {
					{address: sa, nonce: 1},
					{address: sa, nonce: 1},
				},
			},
		},
	})

	err := pool.Insert(ctx.WithPriority(tx.priority), tx)
	require.ErrorContains(t, err, "duplicate lane")
}

func TestMultiLanePriorityNonceMempool_SelectTieUsesAnchorWeightForNonAnchorLane(t *testing.T) {
	accounts := simtypes.RandomAccounts(rand.New(rand.NewSource(5)), 3)
	a := accounts[0].Address
	b := accounts[1].Address
	c := accounts[2].Address
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	txAB := testTx{id: 600, priority: 10, nonce: 0, address: a}  // anchor sender A, also lane on B
	txB := testTx{id: 601, priority: 10, nonce: 1, address: b}   // anchor sender B
	txC := testTx{id: 602, priority: 10, nonce: 0, address: c}   // anchor sender C
	txA2 := testTx{id: 603, priority: -10, nonce: 1, address: a} // gives txAB weight -10
	txB2 := testTx{id: 604, priority: 2, nonce: 2, address: b}   // gives txB weight 2
	txC2 := testTx{id: 605, priority: -1, nonce: 1, address: c}  // gives txC weight -1

	pool := mempool.NewMultiLanePriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
		TxPriority: mempool.NewDefaultTxPriority(),
		SignerExtractor: multiLaneSignerAdapter{
			lanesByID: map[int][]laneSigner{
				txAB.id: {
					{address: a, nonce: 0},
					{address: b, nonce: 0},
				},
				txB.id: {
					{address: b, nonce: 1},
				},
				txC.id: {
					{address: c, nonce: 0},
				},
				txA2.id: {
					{address: a, nonce: 1},
				},
				txB2.id: {
					{address: b, nonce: 2},
				},
				txC2.id: {
					{address: c, nonce: 1},
				},
			},
		},
	})

	for _, tx := range []testTx{txAB, txB, txC, txA2, txB2, txC2} {
		require.NoError(t, pool.Insert(ctx.WithPriority(tx.priority), tx))
	}

	iter := pool.Select(ctx, nil)
	require.NotNil(t, iter)
	first := iter.Tx().Tx.(testTx)
	require.Equal(t, txC.id, first.id)
}
