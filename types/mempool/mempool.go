package mempool

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Tx struct {
	Tx        sdk.Tx
	GasWanted uint64
}

func NewMempoolTx(tx sdk.Tx, gasWanted uint64) Tx {
	return Tx{
		Tx:        tx,
		GasWanted: gasWanted,
	}
}

type GasTx interface {
	GetGas() uint64
}

type Mempool interface {
	// Insert attempts to insert a Tx into the app-side mempool returning
	// an error upon failure.
	Insert(context.Context, sdk.Tx) error

	// Insert with a custom gas wanted value
	InsertWithGasWanted(context.Context, sdk.Tx, uint64) error

	// Select returns an Iterator over the app-side mempool. If txs are specified,
	// then they shall be incorporated into the Iterator. The Iterator must
	// closed by the caller.
	Select(context.Context, [][]byte) Iterator

	// CountTx returns the number of transactions currently in the mempool.
	CountTx() int

	// Remove attempts to remove a transaction from the mempool, returning an error
	// upon failure.
	Remove(sdk.Tx) error
}

// Iterator defines an app-side mempool iterator interface that is as minimal as
// possible. The order of iteration is determined by the app-side mempool
// implementation.
type Iterator interface {
	// Next returns the next transaction from the mempool. If there are no more
	// transactions, it returns nil.
	Next() Iterator

	// Tx returns the transaction at the current position of the iterator.
	Tx() Tx
}

var (
	ErrTxNotFound           = errors.New("tx not found in mempool")
	ErrMempoolTxMaxCapacity = errors.New("pool reached max tx capacity")
)
