package baseapp

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/store/types"
)

type TxExecutor func(
	ctx context.Context,
	block []sdk.Tx,
	cms types.MultiStore,
	deliverTxWithMultiStore func(int, types.MultiStore, map[string]any) *abci.ExecTxResult,
) ([]*abci.ExecTxResult, error)
