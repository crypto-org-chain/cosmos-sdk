package keeper

import (
    "context"

    "cosmossdk.io/errors"

    govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
    "github.com/cosmos/cosmos-sdk/x/mintsupply/types"
)

var _ types.MsgServer = msgServer{}

// msgServer is a wrapper of Keeper.
type msgServer struct {
    Keeper
}

// NewMsgServerImpl returns an implementation of the x/mintsupply MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
    return &msgServer{Keeper: keeper}
}

// UpdateParams updates the params.
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
    if ms.authority != msg.Authority {
        return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.authority, msg.Authority)
    }

    if err := msg.Params.Validate(); err != nil {
        return nil, err
    }

    if err := ms.SetParams(ctx, msg.Params); err != nil {
        return nil, err
    }

    return &types.MsgUpdateParamsResponse{}, nil
}
