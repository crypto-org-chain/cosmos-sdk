package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/x/mintsupply/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns an implementation of the mintsupply QueryServer interface
// for the provided Keeper.
func NewQueryServerImpl(keeper Keeper) types.QueryServer {
	return queryServer{keeper}
}

type queryServer struct {
	keeper Keeper
}

// Params returns the total set of mintsupply parameters.
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// MaxSupply returns the maximum supply of tokens.
func (q queryServer) MaxSupply(ctx context.Context, req *types.QueryMaxSupplyRequest) (*types.QueryMaxSupplyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryMaxSupplyResponse{MaxSupply: params.MaxSupply}, nil
}
