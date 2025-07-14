package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/x/mintsupply/types"
)

// InitGenesis initializes the mintsupply module's state from a provided genesis
// state.
func (k Keeper) InitGenesis(ctx context.Context, genState *types.GenesisState) error {
	// Validate genesis state
	if err := genState.Validate(); err != nil {
		return fmt.Errorf("failed to validate mintsupply genesis state: %w", err)
	}

	// Set the module parameters
	if err := k.SetParams(ctx, genState.Params); err != nil {
		return fmt.Errorf("failed to set mintsupply params: %w", err)
	}

	k.Logger().Info("mintsupply module genesis initialized",
		"max_supply", genState.Params.MaxSupply.String(),
	)

	return nil
}

// ExportGenesis returns the mintsupply module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	// Get current parameters
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get mintsupply params: %w", err)
	}

	// Create and return genesis state
	genState := &types.GenesisState{
		Params: params,
	}

	// Validate the exported genesis state
	if err := genState.Validate(); err != nil {
		return nil, fmt.Errorf("exported mintsupply genesis state is invalid: %w", err)
	}

	k.Logger().Info("mintsupply module genesis exported",
		"max_supply", params.MaxSupply.String(),
	)

	return genState, nil
}
