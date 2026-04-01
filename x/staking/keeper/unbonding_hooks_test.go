package keeper_test

import (
	"time"

	"go.uber.org/mock/gomock"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtestutil "github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (s *KeeperTestSuite) TestAfterUnbondingCompletedHook() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	ctrl := gomock.NewController(s.T())
	mockHooks := stakingtestutil.NewMockStakingHooks(ctrl)
	keeper.SetHooks(mockHooks)

	delAddrs, valAddrs := createValAddrs(1)
	delAddr := delAddrs[0]
	valAddr := valAddrs[0]

	// Create an unbonding delegation with a mature entry (completion time in the past).
	completionTime := ctx.BlockHeader().Time.Add(-time.Hour)
	unbondingId := uint64(42)

	ubd := stakingtypes.NewUnbondingDelegation(
		delAddr, valAddr, 0,
		completionTime,
		sdkmath.NewInt(100),
		unbondingId,
		keeper.ValidatorAddressCodec(),
		s.accountKeeper.AddressCodec(),
	)
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))
	require.NoError(keeper.SetUnbondingDelegationByUnbondingID(ctx, ubd, unbondingId))

	// Expect bank to return coins to delegator.
	s.bankKeeper.EXPECT().
		UndelegateCoinsFromModuleToAccount(gomock.Any(), stakingtypes.NotBondedPoolName, delAddr, gomock.Any()).
		Return(nil)

	// Expect the hook to fire with the correct args.
	mockHooks.EXPECT().
		AfterUnbondingCompleted(gomock.Any(), delAddr, valAddr, []uint64{unbondingId}).
		Return(nil).
		Times(1)

	balances, err := keeper.CompleteUnbonding(ctx, delAddr, valAddr)
	require.NoError(err)
	require.True(balances.IsAllPositive())
}

func (s *KeeperTestSuite) TestAfterRedelegationCompletedHook() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	ctrl := gomock.NewController(s.T())
	mockHooks := stakingtestutil.NewMockStakingHooks(ctrl)
	keeper.SetHooks(mockHooks)

	_, valAddrs := createValAddrs(2)
	valSrcAddr := valAddrs[0]
	valDstAddr := valAddrs[1]
	delAddr := sdk.AccAddress(valSrcAddr)

	// Create a redelegation with a mature entry.
	completionTime := ctx.BlockHeader().Time.Add(-time.Hour)
	unbondingId := uint64(99)

	red := stakingtypes.NewRedelegation(
		delAddr, valSrcAddr, valDstAddr, 0,
		completionTime,
		sdkmath.NewInt(100),
		sdkmath.LegacyNewDec(100),
		unbondingId,
		keeper.ValidatorAddressCodec(),
		s.accountKeeper.AddressCodec(),
	)
	require.NoError(keeper.SetRedelegation(ctx, red))
	require.NoError(keeper.SetRedelegationByUnbondingID(ctx, red, unbondingId))

	// Expect the hook to fire with the correct args.
	mockHooks.EXPECT().
		AfterRedelegationCompleted(gomock.Any(), delAddr, valSrcAddr, valDstAddr, []uint64{unbondingId}).
		Return(nil).
		Times(1)

	balances, err := keeper.CompleteRedelegation(ctx, delAddr, valSrcAddr, valDstAddr)
	require.NoError(err)
	require.True(balances.IsAllPositive())
}
