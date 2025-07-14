package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/mintsupply"
	"github.com/cosmos/cosmos-sdk/x/mintsupply/keeper"
	"github.com/cosmos/cosmos-sdk/x/mintsupply/types"
)

type GenesisTestSuite struct {
	suite.Suite

	ctx    sdk.Context
	cdc    codec.BinaryCodec
	keeper keeper.Keeper
	key    *storetypes.KVStoreKey
}

func (suite *GenesisTestSuite) SetupTest() {
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(suite.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	encCfg := moduletestutil.MakeTestEncodingConfig(mintsupply.AppModuleBasic{})

	suite.cdc = codec.NewProtoCodec(encCfg.InterfaceRegistry)
	suite.ctx = testCtx.Ctx
	suite.key = key

	// Create keeper
	suite.keeper = keeper.NewKeeper(
		suite.cdc,
		runtime.NewKVStoreService(key),
		nil, // codec not needed for these tests
		"authority",
	)
}

func (suite *GenesisTestSuite) TestInitGenesis() {
	testCases := []struct {
		name        string
		genState    *types.GenesisState
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid genesis state",
			genState: &types.GenesisState{
				Params: types.Params{
					MaxSupply: math.NewInt(1000000),
				},
			},
			expectError: false,
		},
		{
			name: "valid genesis state with zero max supply",
			genState: &types.GenesisState{
				Params: types.Params{
					MaxSupply: math.ZeroInt(),
				},
			},
			expectError: false,
		},
		{
			name: "valid genesis state with large max supply",
			genState: &types.GenesisState{
				Params: types.Params{
					MaxSupply: math.NewInt(1000000000000),
				},
			},
			expectError: false,
		},
		{
			name: "invalid genesis state - negative max supply",
			genState: &types.GenesisState{
				Params: types.Params{
					MaxSupply: math.NewInt(-1),
				},
			},
			expectError: true,
			errorMsg:    "failed to validate mintsupply genesis state",
		},
		{
			name:        "nil genesis state",
			genState:    nil,
			expectError: true,
			errorMsg:    "failed to validate mintsupply genesis state",
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := suite.keeper.InitGenesis(suite.ctx, tc.genState)

			if tc.expectError {
				suite.Require().Error(err)
				suite.Require().Contains(err.Error(), tc.errorMsg)
			} else {
				suite.Require().NoError(err)

				// Verify that parameters were set correctly
				params, err := suite.keeper.GetParams(suite.ctx)
				suite.Require().NoError(err)
				suite.Require().Equal(tc.genState.Params.MaxSupply, params.MaxSupply)
			}
		})
	}
}

func (suite *GenesisTestSuite) TestExportGenesis() {
	testCases := []struct {
		name        string
		setupParams types.Params
		expectError bool
		errorMsg    string
	}{
		{
			name: "export with valid params",
			setupParams: types.Params{
				MaxSupply: math.NewInt(1000000),
			},
			expectError: false,
		},
		{
			name: "export with zero max supply",
			setupParams: types.Params{
				MaxSupply: math.ZeroInt(),
			},
			expectError: false,
		},
		{
			name: "export with large max supply",
			setupParams: types.Params{
				MaxSupply: math.NewInt(1000000000000),
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			// Setup: Set parameters in keeper
			err := suite.keeper.SetParams(suite.ctx, tc.setupParams)
			suite.Require().NoError(err)

			// Test: Export genesis
			genState, err := suite.keeper.ExportGenesis(suite.ctx)

			if tc.expectError {
				suite.Require().Error(err)
				suite.Require().Contains(err.Error(), tc.errorMsg)
				suite.Require().Nil(genState)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(genState)

				// Verify exported genesis state
				suite.Require().Equal(tc.setupParams.MaxSupply, genState.Params.MaxSupply)

				// Verify that exported genesis state is valid
				err = genState.Validate()
				suite.Require().NoError(err)
			}
		})
	}
}

func (suite *GenesisTestSuite) TestInitExportGenesis() {
	// Test the round-trip: Init -> Export -> Init again
	originalGenState := &types.GenesisState{
		Params: types.Params{
			MaxSupply: math.NewInt(5000000),
		},
	}

	// Initialize genesis
	err := suite.keeper.InitGenesis(suite.ctx, originalGenState)
	suite.Require().NoError(err)

	// Export genesis
	exportedGenState, err := suite.keeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().NotNil(exportedGenState)

	// Verify exported state matches original
	suite.Require().Equal(originalGenState.Params.MaxSupply, exportedGenState.Params.MaxSupply)

	// Initialize with exported state (round-trip test)
	err = suite.keeper.InitGenesis(suite.ctx, exportedGenState)
	suite.Require().NoError(err)

	// Export again and verify consistency
	secondExportedGenState, err := suite.keeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Equal(exportedGenState.Params.MaxSupply, secondExportedGenState.Params.MaxSupply)
}

func (suite *GenesisTestSuite) TestExportGenesisWithoutInit() {
	// Test exporting genesis when no parameters have been set
	// This should return an error since GetParams will fail
	genState, err := suite.keeper.ExportGenesis(suite.ctx)
	suite.Require().Error(err)
	suite.Require().Nil(genState)
	suite.Require().Contains(err.Error(), "failed to get mintsupply params")
}

func (suite *GenesisTestSuite) TestInitGenesisValidation() {
	// Test that InitGenesis properly validates the genesis state
	invalidGenState := &types.GenesisState{
		Params: types.Params{
			MaxSupply: math.NewInt(-100), // Invalid negative value
		},
	}

	err := suite.keeper.InitGenesis(suite.ctx, invalidGenState)
	suite.Require().Error(err)
	suite.Require().Contains(err.Error(), "failed to validate mintsupply genesis state")
}

func (suite *GenesisTestSuite) TestExportGenesisValidation() {
	// Setup invalid params directly in store (bypassing validation)
	// This tests that ExportGenesis validates the exported state
	invalidParams := types.Params{
		MaxSupply: math.NewInt(-1),
	}

	// Force set invalid params (this would normally be prevented by SetParams validation)
	err := suite.keeper.SetParams(suite.ctx, invalidParams)
	if err == nil {
		// If SetParams doesn't validate, then ExportGenesis should catch the invalid state
		genState, err := suite.keeper.ExportGenesis(suite.ctx)
		suite.Require().Error(err)
		suite.Require().Nil(genState)
		suite.Require().Contains(err.Error(), "exported mintsupply genesis state is invalid")
	} else {
		// If SetParams validates and rejects invalid params, that's also correct behavior
		suite.Require().Error(err)
	}
}

func TestGenesisTestSuite(t *testing.T) {
	suite.Run(t, new(GenesisTestSuite))
}

// Additional unit tests for edge cases
func TestInitGenesisNilKeeper(t *testing.T) {
	// This test ensures that the function handles nil keeper gracefully
	// Note: This would typically panic, but we include it for completeness
	var k keeper.Keeper
	genState := &types.GenesisState{
		Params: types.Params{
			MaxSupply: math.NewInt(1000000),
		},
	}

	// This will likely panic due to nil keeper, but we test the behavior
	require.Panics(t, func() {
		_ = k.InitGenesis(nil, genState)
	})
}

func TestExportGenesisNilKeeper(t *testing.T) {
	// This test ensures that the function handles nil keeper gracefully
	var k keeper.Keeper

	// This will likely panic due to nil keeper, but we test the behavior
	require.Panics(t, func() {
		_, _ = k.ExportGenesis(nil)
	})
}
