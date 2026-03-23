package keeper_test

import (
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	"go.uber.org/mock/gomock"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtestutil "github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (s *KeeperTestSuite) TestSlashUnbondingDelegationHook() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	// Setup mock hooks
	ctrl := gomock.NewController(s.T())
	mockHooks := stakingtestutil.NewMockStakingHooks(ctrl)
	keeper.SetHooks(mockHooks)

	delAddrs, valAddrs := createValAddrs(1)
	delAddr := delAddrs[0]
	valAddr := valAddrs[0]

	// Use a completion time far in the future so the entry is not mature
	completionTime := ctx.BlockHeader().Time.Add(time.Hour * 24)

	// Create an unbonding delegation entry
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddr, valAddr, 0,
		completionTime,
		sdkmath.NewInt(100),
		1,
		keeper.ValidatorAddressCodec(),
		s.accountKeeper.AddressCodec(),
	)
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))

	slashFactor := sdkmath.LegacyNewDecWithPrec(5, 1) // 50%

	// Expect the AfterSlashUnbondingDelegation hook to be called
	mockHooks.EXPECT().
		AfterSlashUnbondingDelegation(gomock.Any(), uint64(1), gomock.Any()).
		Return(nil).
		Times(1)

	// Expect burn of not-bonded tokens
	s.bankKeeper.EXPECT().
		BurnCoins(gomock.Any(), stakingtypes.NotBondedPoolName, gomock.Any()).
		Return(nil).
		Times(1)

	totalSlashed, err := keeper.SlashUnbondingDelegation(ctx, ubd, 0, slashFactor)
	require.NoError(err)
	require.True(totalSlashed.IsPositive())

	// Verify the unbonding delegation entry balance was reduced
	updatedUbd, err := keeper.GetUnbondingDelegation(ctx, delAddr, valAddr)
	require.NoError(err)
	require.Equal(sdkmath.NewInt(50), updatedUbd.Entries[0].Balance)
}

func (s *KeeperTestSuite) TestSlashUnbondingDelegationHookErrorAbortsSlash() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	// Setup mock hooks
	ctrl := gomock.NewController(s.T())
	mockHooks := stakingtestutil.NewMockStakingHooks(ctrl)
	keeper.SetHooks(mockHooks)

	delAddrs, valAddrs := createValAddrs(1)
	delAddr := delAddrs[0]
	valAddr := valAddrs[0]

	completionTime := ctx.BlockHeader().Time.Add(time.Hour * 24)

	// Create an unbonding delegation entry
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddr, valAddr, 0,
		completionTime,
		sdkmath.NewInt(100),
		1,
		keeper.ValidatorAddressCodec(),
		s.accountKeeper.AddressCodec(),
	)
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))

	slashFactor := sdkmath.LegacyNewDecWithPrec(5, 1) // 50%

	// Hook returns an error - slash should be aborted and error propagated
	mockHooks.EXPECT().
		AfterSlashUnbondingDelegation(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("hook error")).
		Times(1)

	// Burn should NOT happen because hook error aborts slash
	// (no BurnCoins expectation set)

	_, err := keeper.SlashUnbondingDelegation(ctx, ubd, 0, slashFactor)
	require.ErrorContains(err, "hook error")
}

func (s *KeeperTestSuite) TestUndelegateReturnsUnbondingId() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delAddrs, valAddrs := createValAddrs(1)
	delAddr := delAddrs[0]
	valAddr := valAddrs[0]

	// Create validator
	validator := stakingtestutil.NewValidator(s.T(), valAddr, PKs[0])
	tokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(tokens)
	require.Equal(tokens, issuedShares.TruncateInt())

	// TestingUpdateValidator with apply=true triggers notBondedToBonded
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any()).Return(nil)
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)

	// Create delegation
	delegation := stakingtypes.NewDelegation(delAddr.String(), valAddr.String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	// Expect bank operation for undelegation (bonded to not bonded)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any()).Return(nil)

	// Undelegate
	shares := sdkmath.LegacyNewDecFromInt(keeper.TokensFromConsensusPower(ctx, 5))
	completionTime, returnAmount, unbondingId, err := keeper.Undelegate(ctx, delAddr, valAddr, shares)
	require.NoError(err)
	require.True(returnAmount.IsPositive())
	require.False(completionTime.IsZero())

	// Verify unbondingId is valid (non-zero)
	require.NotZero(unbondingId)

	// Verify the unbondingId matches what's stored
	ubd, err := keeper.GetUnbondingDelegation(ctx, delAddr, valAddr)
	require.NoError(err)
	require.Len(ubd.Entries, 1)
	require.Equal(unbondingId, ubd.Entries[0].UnbondingId)
}

func (s *KeeperTestSuite) TestBeginRedelegationReturnsUnbondingIdAndShares() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	_, valAddrs := createValAddrs(2)
	valSrcAddr := valAddrs[0]
	valDstAddr := valAddrs[1]
	delAddr := sdk.AccAddress(valSrcAddr)

	// Create source validator
	srcValidator := stakingtestutil.NewValidator(s.T(), valSrcAddr, PKs[0])
	srcTokens := keeper.TokensFromConsensusPower(ctx, 10)
	srcValidator, issuedShares := srcValidator.AddTokensFromDel(srcTokens)

	// Apply validators (triggers notBondedToBonded for each)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any()).Return(nil)
	srcValidator = stakingkeeper.TestingUpdateValidator(keeper, ctx, srcValidator, true)

	dstValidator := stakingtestutil.NewValidator(s.T(), valDstAddr, PKs[1])
	dstTokens := keeper.TokensFromConsensusPower(ctx, 10)
	dstValidator, _ = dstValidator.AddTokensFromDel(dstTokens)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any()).Return(nil)
	dstValidator = stakingkeeper.TestingUpdateValidator(keeper, ctx, dstValidator, true)

	// Create delegation to source
	delegation := stakingtypes.NewDelegation(delAddr.String(), valSrcAddr.String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	// Begin redelegation
	shares := sdkmath.LegacyNewDecFromInt(keeper.TokensFromConsensusPower(ctx, 5))
	completionTime, newShares, unbondingId, err := keeper.BeginRedelegation(ctx, delAddr, valSrcAddr, valDstAddr, shares)
	require.NoError(err)
	require.False(completionTime.IsZero())
	require.True(newShares.IsPositive())

	// Verify unbondingId is valid (non-zero)
	require.NotZero(unbondingId)

	// Verify the redelegation was created with the correct unbondingId
	red, err := keeper.GetRedelegation(ctx, delAddr, valSrcAddr, valDstAddr)
	require.NoError(err)
	require.Len(red.Entries, 1)
	require.Equal(unbondingId, red.Entries[0].UnbondingId)
}

func (s *KeeperTestSuite) TestSlashRedelegationBurnsTokens() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	_, valAddrs := createValAddrs(2)
	valSrcAddr := valAddrs[0]
	valDstAddr := valAddrs[1]
	delAddr := sdk.AccAddress(valSrcAddr)

	// Create source validator (bonded)
	srcValidator := stakingtestutil.NewValidator(s.T(), valSrcAddr, PKs[0])
	srcTokens := keeper.TokensFromConsensusPower(ctx, 10)
	srcValidator, issuedShares := srcValidator.AddTokensFromDel(srcTokens)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any()).Return(nil)
	srcValidator = stakingkeeper.TestingUpdateValidator(keeper, ctx, srcValidator, true)

	// Create destination validator (bonded)
	dstValidator := stakingtestutil.NewValidator(s.T(), valDstAddr, PKs[1])
	dstTokens := keeper.TokensFromConsensusPower(ctx, 10)
	dstValidator, _ = dstValidator.AddTokensFromDel(dstTokens)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any()).Return(nil)
	dstValidator = stakingkeeper.TestingUpdateValidator(keeper, ctx, dstValidator, true)

	// Create delegation to source
	delegation := stakingtypes.NewDelegation(delAddr.String(), valSrcAddr.String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	// Begin redelegation to create the redelegation entry
	shares := sdkmath.LegacyNewDecFromInt(keeper.TokensFromConsensusPower(ctx, 5))
	_, _, _, err := keeper.BeginRedelegation(ctx, delAddr, valSrcAddr, valDstAddr, shares)
	require.NoError(err)

	// Get the redelegation
	red, err := keeper.GetRedelegation(ctx, delAddr, valSrcAddr, valDstAddr)
	require.NoError(err)

	slashFactor := sdkmath.LegacyNewDecWithPrec(5, 1) // 50%

	// Expect BurnCoins for bonded tokens (destination validator is bonded)
	s.bankKeeper.EXPECT().
		BurnCoins(gomock.Any(), stakingtypes.BondedPoolName, gomock.Any()).
		Return(nil)

	// Slash the redelegation - this should burn tokens (the critical fix)
	totalSlashed, err := keeper.SlashRedelegation(ctx, srcValidator, red, 0, slashFactor)
	require.NoError(err)
	require.True(totalSlashed.IsPositive())
}
