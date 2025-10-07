package keeper_test

import (
	"time"

	"github.com/golang/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec/address"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func createValAddrs(count int) ([]sdk.AccAddress, []sdk.ValAddress) {
	addrs := simtestutil.CreateIncrementalAccounts(count)
	valAddrs := simtestutil.ConvertAddrsToValAddrs(addrs)

	return addrs, valAddrs
}

// tests GetDelegation, GetDelegatorDelegations, SetDelegation, RemoveDelegation, GetDelegatorDelegations
func (s *KeeperTestSuite) TestDelegation() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, valAddrs := createValAddrs(3)

	s.accountKeeper.EXPECT().AddressCodec().Return(address.NewBech32Codec("cosmos")).AnyTimes()

	// construct the validators
	amts := []math.Int{math.NewInt(9), math.NewInt(8), math.NewInt(7)}
	var validators [3]stakingtypes.Validator
	for i, amt := range amts {
		validators[i] = testutil.NewValidator(s.T(), valAddrs[i], PKs[i])
		validators[i], _ = validators[i].AddTokensFromDel(amt)

		validators[i] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[i], true)
	}

	// first add a validators[0] to delegate too
	bond1to1 := stakingtypes.NewDelegation(addrDels[0].String(), valAddrs[0].String(), math.LegacyNewDec(9))

	// check the empty keeper first
	_, err := keeper.GetDelegation(ctx, addrDels[0], valAddrs[0])
	require.ErrorIs(err, stakingtypes.ErrNoDelegation)

	// set and retrieve a record
	require.NoError(keeper.SetDelegation(ctx, bond1to1))
	resBond, err := keeper.GetDelegation(ctx, addrDels[0], valAddrs[0])
	require.NoError(err)
	require.Equal(bond1to1, resBond)

	// modify a records, save, and retrieve
	bond1to1.Shares = math.LegacyNewDec(99)
	require.NoError(keeper.SetDelegation(ctx, bond1to1))
	resBond, err = keeper.GetDelegation(ctx, addrDels[0], valAddrs[0])
	require.NoError(err)
	require.Equal(bond1to1, resBond)

	// add some more records
	bond1to2 := stakingtypes.NewDelegation(addrDels[0].String(), valAddrs[1].String(), math.LegacyNewDec(9))
	bond1to3 := stakingtypes.NewDelegation(addrDels[0].String(), valAddrs[2].String(), math.LegacyNewDec(9))
	bond2to1 := stakingtypes.NewDelegation(addrDels[1].String(), valAddrs[0].String(), math.LegacyNewDec(9))
	bond2to2 := stakingtypes.NewDelegation(addrDels[1].String(), valAddrs[1].String(), math.LegacyNewDec(9))
	bond2to3 := stakingtypes.NewDelegation(addrDels[1].String(), valAddrs[2].String(), math.LegacyNewDec(9))
	require.NoError(keeper.SetDelegation(ctx, bond1to2))
	require.NoError(keeper.SetDelegation(ctx, bond1to3))
	require.NoError(keeper.SetDelegation(ctx, bond2to1))
	require.NoError(keeper.SetDelegation(ctx, bond2to2))
	require.NoError(keeper.SetDelegation(ctx, bond2to3))

	// test all bond retrieve capabilities
	resBonds, err := keeper.GetDelegatorDelegations(ctx, addrDels[0], 5)
	require.NoError(err)
	require.Equal(3, len(resBonds))
	require.Equal(bond1to1, resBonds[0])
	require.Equal(bond1to2, resBonds[1])
	require.Equal(bond1to3, resBonds[2])
	resBonds, err = keeper.GetAllDelegatorDelegations(ctx, addrDels[0])
	require.NoError(err)
	require.Equal(3, len(resBonds))
	resBonds, err = keeper.GetDelegatorDelegations(ctx, addrDels[0], 2)
	require.NoError(err)
	require.Equal(2, len(resBonds))
	resBonds, err = keeper.GetDelegatorDelegations(ctx, addrDels[1], 5)
	require.NoError(err)
	require.Equal(3, len(resBonds))
	require.Equal(bond2to1, resBonds[0])
	require.Equal(bond2to2, resBonds[1])
	require.Equal(bond2to3, resBonds[2])
	allBonds, err := keeper.GetAllDelegations(ctx)
	require.NoError(err)
	require.Equal(6, len(allBonds))
	require.Equal(bond1to1, allBonds[0])
	require.Equal(bond1to2, allBonds[1])
	require.Equal(bond1to3, allBonds[2])
	require.Equal(bond2to1, allBonds[3])
	require.Equal(bond2to2, allBonds[4])
	require.Equal(bond2to3, allBonds[5])

	resVals, err := keeper.GetDelegatorValidators(ctx, addrDels[0], 3)
	require.NoError(err)
	require.Equal(3, len(resVals.Validators))
	resVals, err = keeper.GetDelegatorValidators(ctx, addrDels[1], 4)
	require.NoError(err)
	require.Equal(3, len(resVals.Validators))

	for i := 0; i < 3; i++ {
		resVal, err := keeper.GetDelegatorValidator(ctx, addrDels[0], valAddrs[i])
		require.Nil(err)
		require.Equal(valAddrs[i].String(), resVal.GetOperator())

		resVal, err = keeper.GetDelegatorValidator(ctx, addrDels[1], valAddrs[i])
		require.Nil(err)
		require.Equal(valAddrs[i].String(), resVal.GetOperator())

		resDels, err := keeper.GetValidatorDelegations(ctx, valAddrs[i])
		require.NoError(err)
		require.Len(resDels, 2)
	}

	// test total bonded for single delegator
	expBonded := bond1to1.Shares.Add(bond2to1.Shares).Add(bond1to3.Shares)
	resDelBond, err := keeper.GetDelegatorBonded(ctx, addrDels[0])
	require.NoError(err)
	require.Equal(expBonded, math.LegacyNewDecFromInt(resDelBond))

	// delete a record
	require.NoError(keeper.RemoveDelegation(ctx, bond2to3))
	_, err = keeper.GetDelegation(ctx, addrDels[1], valAddrs[2])
	require.ErrorIs(err, stakingtypes.ErrNoDelegation)
	resBonds, err = keeper.GetDelegatorDelegations(ctx, addrDels[1], 5)
	require.NoError(err)
	require.Equal(2, len(resBonds))
	require.Equal(bond2to1, resBonds[0])
	require.Equal(bond2to2, resBonds[1])

	resBonds, err = keeper.GetAllDelegatorDelegations(ctx, addrDels[1])
	require.NoError(err)
	require.Equal(2, len(resBonds))

	// delete all the records from delegator 2
	require.NoError(keeper.RemoveDelegation(ctx, bond2to1))
	require.NoError(keeper.RemoveDelegation(ctx, bond2to2))
	_, err = keeper.GetDelegation(ctx, addrDels[1], valAddrs[0])
	require.ErrorIs(err, stakingtypes.ErrNoDelegation)
	_, err = keeper.GetDelegation(ctx, addrDels[1], valAddrs[1])
	require.ErrorIs(err, stakingtypes.ErrNoDelegation)
	resBonds, err = keeper.GetDelegatorDelegations(ctx, addrDels[1], 5)
	require.NoError(err)
	require.Equal(0, len(resBonds))
}

func (s *KeeperTestSuite) TestDelegationsByValIndex() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, valAddrs := createValAddrs(3)

	for _, addr := range addrDels {
		s.bankKeeper.EXPECT().DelegateCoinsFromAccountToModule(gomock.Any(), addr, gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	s.accountKeeper.EXPECT().AddressCodec().Return(address.NewBech32Codec("cosmos")).AnyTimes()

	// construct the validators
	amts := []math.Int{math.NewInt(9), math.NewInt(8), math.NewInt(7)}
	var validators [3]stakingtypes.Validator
	for i, amt := range amts {
		validators[i] = testutil.NewValidator(s.T(), valAddrs[i], PKs[i])
		validators[i], _ = validators[i].AddTokensFromDel(amt)

		validators[i] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[i], true)
	}

	// delegate 2 tokens
	//
	// total delegations after delegating: del1 -> 2stake
	_, err := s.msgServer.Delegate(ctx, stakingtypes.NewMsgDelegate(addrDels[0].String(), valAddrs[0].String(), sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(2))))
	require.NoError(err)

	dels, err := s.stakingKeeper.GetValidatorDelegations(ctx, valAddrs[0])
	require.NoError(err)
	require.Len(dels, 1)

	// delegate 4 tokens
	//
	// total delegations after delegating: del1 -> 2stake, del2 -> 4stake
	_, err = s.msgServer.Delegate(ctx, stakingtypes.NewMsgDelegate(addrDels[1].String(), valAddrs[0].String(), sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(4))))
	require.NoError(err)

	dels, err = s.stakingKeeper.GetValidatorDelegations(ctx, valAddrs[0])
	require.NoError(err)
	require.Len(dels, 2)

	// undelegate 1 token from del1
	//
	// total delegations after undelegating: del1 -> 1stake, del2 -> 4stake
	_, err = s.msgServer.Undelegate(ctx, stakingtypes.NewMsgUndelegate(addrDels[0].String(), valAddrs[0].String(), sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1))))
	require.NoError(err)

	dels, err = s.stakingKeeper.GetValidatorDelegations(ctx, valAddrs[0])
	require.NoError(err)
	require.Len(dels, 2)

	// undelegate 1 token from del1
	//
	// total delegations after undelegating: del2 -> 4stake
	_, err = s.msgServer.Undelegate(ctx, stakingtypes.NewMsgUndelegate(addrDels[0].String(), valAddrs[0].String(), sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1))))
	require.NoError(err)

	dels, err = s.stakingKeeper.GetValidatorDelegations(ctx, valAddrs[0])
	require.NoError(err)
	require.Len(dels, 1)

	// undelegate 2 tokens from del2
	//
	// total delegations after undelegating: del2 -> 2stake
	_, err = s.msgServer.Undelegate(ctx, stakingtypes.NewMsgUndelegate(addrDels[1].String(), valAddrs[0].String(), sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(2))))
	require.NoError(err)

	dels, err = s.stakingKeeper.GetValidatorDelegations(ctx, valAddrs[0])
	require.NoError(err)
	require.Len(dels, 1)

	// undelegate 2 tokens from del2
	//
	// total delegations after undelegating: []
	_, err = s.msgServer.Undelegate(ctx, stakingtypes.NewMsgUndelegate(addrDels[1].String(), valAddrs[0].String(), sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(2))))
	require.NoError(err)

	dels, err = s.stakingKeeper.GetValidatorDelegations(ctx, valAddrs[0])
	require.NoError(err)
	require.Len(dels, 0)
}

// tests Get/Set/Remove UnbondingDelegation
func (s *KeeperTestSuite) TestUnbondingDelegation() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delAddrs, valAddrs := createValAddrs(2)

	s.accountKeeper.EXPECT().AddressCodec().Return(address.NewBech32Codec("cosmos")).AnyTimes()

	ubd := stakingtypes.NewUnbondingDelegation(
		delAddrs[0],
		valAddrs[0],
		0,
		time.Unix(0, 0).UTC(),
		math.NewInt(5),
		address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"),
	)

	// set and retrieve a record
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))
	resUnbond, err := keeper.GetUnbondingDelegation(ctx, delAddrs[0], valAddrs[0])
	require.NoError(err)
	require.Equal(ubd, resUnbond)

	// modify a records, save, and retrieve
	expUnbond := math.NewInt(21)
	ubd.Entries[0].Balance = expUnbond
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))

	resUnbonds, err := keeper.GetUnbondingDelegations(ctx, delAddrs[0], 5)
	require.NoError(err)
	require.Equal(1, len(resUnbonds))

	resUnbonds, err = keeper.GetAllUnbondingDelegations(ctx, delAddrs[0])
	require.NoError(err)
	require.Equal(1, len(resUnbonds))

	resUnbond, err = keeper.GetUnbondingDelegation(ctx, delAddrs[0], valAddrs[0])
	require.NoError(err)
	require.Equal(ubd, resUnbond)

	resDelUnbond, err := keeper.GetDelegatorUnbonding(ctx, delAddrs[0])
	require.NoError(err)
	require.Equal(expUnbond, resDelUnbond)

	// delete a record
	require.NoError(keeper.RemoveUnbondingDelegation(ctx, ubd))
	_, err = keeper.GetUnbondingDelegation(ctx, delAddrs[0], valAddrs[0])
	require.ErrorIs(err, stakingtypes.ErrNoUnbondingDelegation)

	resUnbonds, err = keeper.GetUnbondingDelegations(ctx, delAddrs[0], 5)
	require.NoError(err)
	require.Equal(0, len(resUnbonds))

	resUnbonds, err = keeper.GetAllUnbondingDelegations(ctx, delAddrs[0])
	require.NoError(err)
	require.Equal(0, len(resUnbonds))
}

func (s *KeeperTestSuite) TestUnbondingDelegationsFromValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delAddrs, valAddrs := createValAddrs(2)

	ubd := stakingtypes.NewUnbondingDelegation(
		delAddrs[0],
		valAddrs[0],
		0,
		time.Unix(0, 0).UTC(),
		math.NewInt(5),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)

	// set and retrieve a record
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))
	resUnbond, err := keeper.GetUnbondingDelegation(ctx, delAddrs[0], valAddrs[0])
	require.NoError(err)
	require.Equal(ubd, resUnbond)

	// modify a records, save, and retrieve
	expUnbond := math.NewInt(21)
	ubd.Entries[0].Balance = expUnbond
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))

	resUnbonds, err := keeper.GetUnbondingDelegations(ctx, delAddrs[0], 5)
	require.NoError(err)
	require.Equal(1, len(resUnbonds))

	resUnbonds, err = keeper.GetAllUnbondingDelegations(ctx, delAddrs[0])
	require.NoError(err)
	require.Equal(1, len(resUnbonds))

	resUnbonds, err = keeper.GetUnbondingDelegationsFromValidator(ctx, valAddrs[0])
	require.NoError(err)
	require.Equal(1, len(resUnbonds))

	resUnbond, err = keeper.GetUnbondingDelegation(ctx, delAddrs[0], valAddrs[0])
	require.NoError(err)
	require.Equal(ubd, resUnbond)

	resDelUnbond, err := keeper.GetDelegatorUnbonding(ctx, delAddrs[0])
	require.NoError(err)
	require.Equal(expUnbond, resDelUnbond)

	// delete a record
	require.NoError(keeper.RemoveUnbondingDelegation(ctx, ubd))
	_, err = keeper.GetUnbondingDelegation(ctx, delAddrs[0], valAddrs[0])
	require.ErrorIs(err, stakingtypes.ErrNoUnbondingDelegation)

	resUnbonds, err = keeper.GetUnbondingDelegations(ctx, delAddrs[0], 5)
	require.NoError(err)
	require.Equal(0, len(resUnbonds))

	resUnbonds, err = keeper.GetAllUnbondingDelegations(ctx, delAddrs[0])
	require.NoError(err)
	require.Equal(0, len(resUnbonds))

	resUnbonds, err = keeper.GetUnbondingDelegationsFromValidator(ctx, valAddrs[0])
	require.NoError(err)
	require.Equal(0, len(resUnbonds))
}

func (s *KeeperTestSuite) TestUnbondDelegation() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delAddrs, valAddrs := createValAddrs(1)

	startTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator := testutil.NewValidator(s.T(), valAddrs[0], PKs[0])

	validator, issuedShares := validator.AddTokensFromDel(startTokens)
	require.Equal(startTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	_ = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)

	delegation := stakingtypes.NewDelegation(delAddrs[0].String(), valAddrs[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	bondTokens := keeper.TokensFromConsensusPower(ctx, 6)
	amount, err := keeper.Unbond(ctx, delAddrs[0], valAddrs[0], math.LegacyNewDecFromInt(bondTokens))
	require.NoError(err)
	require.Equal(bondTokens, amount) // shares to be added to an unbonding delegation

	delegation, err = keeper.GetDelegation(ctx, delAddrs[0], valAddrs[0])
	require.NoError(err)
	validator, err = keeper.GetValidator(ctx, valAddrs[0])
	require.NoError(err)

	remainingTokens := startTokens.Sub(bondTokens)

	require.Equal(remainingTokens, delegation.Shares.RoundInt())
	require.Equal(remainingTokens, validator.BondedTokens())
}

// // test undelegating self delegation from a validator pushing it below MinSelfDelegation
// // shift it from the bonded to unbonding state and jailed
func (s *KeeperTestSuite) TestUndelegateSelfDelegationBelowMinSelfDelegation() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, addrVals := createValAddrs(1)
	delTokens := keeper.TokensFromConsensusPower(ctx, 10)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])

	validator.MinSelfDelegation = delTokens
	validator, issuedShares := validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))
	require.True(validator.IsBonded())

	selfDelegation := stakingtypes.NewDelegation(sdk.AccAddress(addrVals[0].Bytes()).String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second delegation to this validator
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.True(validator.IsBonded())
	require.Equal(delTokens, issuedShares.RoundInt())

	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	delegation := stakingtypes.NewDelegation(addrDels[0].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	val0AccAddr := sdk.AccAddress(addrVals[0].Bytes())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	_, _, err := keeper.Undelegate(ctx, val0AccAddr, addrVals[0], math.LegacyNewDecFromInt(keeper.TokensFromConsensusPower(ctx, 6)))
	require.NoError(err)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 1)

	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(keeper.TokensFromConsensusPower(ctx, 14), validator.Tokens)
	require.Equal(stakingtypes.Unbonding, validator.Status)
	require.True(validator.Jailed)
}

func (s *KeeperTestSuite) TestUndelegateFromUnbondingValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()
	delTokens := keeper.TokensFromConsensusPower(ctx, 10)

	addrDels, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	validator, issuedShares := validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.True(validator.IsBonded())

	selfDelegation := stakingtypes.NewDelegation(addrDels[0].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second delegation to this validator
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))

	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())

	stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	delegation := stakingtypes.NewDelegation(addrDels[1].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	header := ctx.BlockHeader()
	blockHeight := int64(10)
	header.Height = blockHeight
	blockTime := time.Unix(333, 0)
	header.Time = blockTime
	ctx = ctx.WithBlockHeader(header)

	// unbond the all self-delegation to put validator in unbonding state
	val0AccAddr := sdk.AccAddress(addrVals[0])
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	_, amount, err := keeper.Undelegate(ctx, val0AccAddr, addrVals[0], math.LegacyNewDecFromInt(delTokens))
	require.NoError(err)
	require.Equal(amount, delTokens)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 1)

	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(blockHeight, validator.UnbondingHeight)
	params, err := keeper.GetParams(ctx)
	require.NoError(err)
	require.True(blockTime.Add(params.UnbondingTime).Equal(validator.UnbondingTime))

	blockHeight2 := int64(20)
	blockTime2 := time.Unix(444, 0).UTC()
	ctx = ctx.WithBlockHeight(blockHeight2)
	ctx = ctx.WithBlockTime(blockTime2)

	// unbond some of the other delegation's shares
	undelegateAmount := math.LegacyNewDec(6)
	_, undelegatedAmount, err := keeper.Undelegate(ctx, addrDels[1], addrVals[0], undelegateAmount)
	require.NoError(err)
	require.Equal(math.LegacyNewDecFromInt(undelegatedAmount), undelegateAmount)

	// retrieve the unbonding delegation
	ubd, err := keeper.GetUnbondingDelegation(ctx, addrDels[1], addrVals[0])
	require.NoError(err)
	require.Len(ubd.Entries, 1)
	require.True(ubd.Entries[0].Balance.Equal(math.NewInt(6)))
	require.Equal(blockHeight2, ubd.Entries[0].CreationHeight)
	require.True(blockTime2.Add(params.UnbondingTime).Equal(ubd.Entries[0].CompletionTime))
}

func (s *KeeperTestSuite) TestUndelegateFromUnbondedValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delTokens := keeper.TokensFromConsensusPower(ctx, 10)
	addrDels, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	valTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.True(validator.IsBonded())

	val0AccAddr := sdk.AccAddress(addrVals[0])
	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second delegation to this validator
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.True(validator.IsBonded())
	delegation := stakingtypes.NewDelegation(addrDels[1].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	ctx = ctx.WithBlockHeight(10)
	ctx = ctx.WithBlockTime(time.Unix(333, 0))

	// unbond the all self-delegation to put validator in unbonding state
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	_, amount, err := keeper.Undelegate(ctx, val0AccAddr, addrVals[0], math.LegacyNewDecFromInt(valTokens))
	require.NoError(err)
	require.Equal(amount, valTokens)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 1)

	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(ctx.BlockHeight(), validator.UnbondingHeight)
	params, err := keeper.GetParams(ctx)
	require.NoError(err)
	require.True(ctx.BlockHeader().Time.Add(params.UnbondingTime).Equal(validator.UnbondingTime))

	// unbond the validator
	ctx = ctx.WithBlockTime(validator.UnbondingTime)
	err = keeper.UnbondAllMatureValidators(ctx)
	require.NoError(err)

	// Make sure validator is still in state because there is still an outstanding delegation
	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(validator.Status, stakingtypes.Unbonded)

	// unbond some of the other delegation's shares
	unbondTokens := keeper.TokensFromConsensusPower(ctx, 6)
	_, amount2, err := keeper.Undelegate(ctx, addrDels[1], addrVals[0], math.LegacyNewDecFromInt(unbondTokens))
	require.NoError(err)
	require.Equal(amount2, unbondTokens)

	// unbond rest of the other delegation's shares
	remainingTokens := delTokens.Sub(unbondTokens)
	_, amount3, err := keeper.Undelegate(ctx, addrDels[1], addrVals[0], math.LegacyNewDecFromInt(remainingTokens))
	require.NoError(err)
	require.Equal(amount3, remainingTokens)

	//  now validator should be deleted from state
	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.ErrorIs(err, stakingtypes.ErrNoValidatorFound)
}

func (s *KeeperTestSuite) TestUnbondingAllDelegationFromValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delTokens := keeper.TokensFromConsensusPower(ctx, 10)
	addrDels, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	valTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.True(validator.IsBonded())
	val0AccAddr := sdk.AccAddress(addrVals[0].Bytes())

	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second delegation to this validator
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())

	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.True(validator.IsBonded())

	delegation := stakingtypes.NewDelegation(addrDels[1].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	ctx = ctx.WithBlockHeight(10)
	ctx = ctx.WithBlockTime(time.Unix(333, 0))

	// unbond the all self-delegation to put validator in unbonding state
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	_, amount, err := keeper.Undelegate(ctx, val0AccAddr, addrVals[0], math.LegacyNewDecFromInt(valTokens))
	require.NoError(err)
	require.Equal(amount, valTokens)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 1)

	// unbond all the remaining delegation
	_, amount2, err := keeper.Undelegate(ctx, addrDels[1], addrVals[0], math.LegacyNewDecFromInt(delTokens))
	require.NoError(err)
	require.Equal(amount2, delTokens)

	// validator should still be in state and still be in unbonding state
	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(validator.Status, stakingtypes.Unbonding)

	// unbond the validator
	ctx = ctx.WithBlockTime(validator.UnbondingTime)
	err = keeper.UnbondAllMatureValidators(ctx)
	require.NoError(err)

	// validator should now be deleted from state
	_, err = keeper.GetValidator(ctx, addrVals[0])
	require.ErrorIs(err, stakingtypes.ErrNoValidatorFound)
}

// Make sure that that the retrieving the delegations doesn't affect the state
func (s *KeeperTestSuite) TestGetRedelegationsFromSrcValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, addrVals := createValAddrs(2)

	rd := stakingtypes.NewRedelegation(addrDels[0], addrVals[0], addrVals[1], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))

	// set and retrieve a record
	err := keeper.SetRedelegation(ctx, rd)
	require.NoError(err)
	resBond, err := keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.NoError(err)

	// get the redelegations one time
	redelegations, err := keeper.GetRedelegationsFromSrcValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resBond)

	// get the redelegations a second time, should be exactly the same
	redelegations, err = keeper.GetRedelegationsFromSrcValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resBond)
}

// tests Get/Set/Remove/Has UnbondingDelegation
func (s *KeeperTestSuite) TestRedelegation() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, addrVals := createValAddrs(2)

	rd := stakingtypes.NewRedelegation(addrDels[0], addrVals[0], addrVals[1], 0,
		time.Unix(0, 0).UTC(), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))

	// test shouldn't have and redelegations
	has, err := keeper.HasReceivingRedelegation(ctx, addrDels[0], addrVals[1])
	require.NoError(err)
	require.False(has)

	// set and retrieve a record
	err = keeper.SetRedelegation(ctx, rd)
	require.NoError(err)
	resRed, err := keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.NoError(err)

	redelegations, err := keeper.GetRedelegationsFromSrcValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resRed)

	redelegations, err = keeper.GetRedelegations(ctx, addrDels[0], 5)
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resRed)

	redelegations, err = keeper.GetAllRedelegations(ctx, addrDels[0], nil, nil)
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resRed)

	// check if has the redelegation
	has, err = keeper.HasReceivingRedelegation(ctx, addrDels[0], addrVals[1])
	require.NoError(err)
	require.True(has)

	// modify a records, save, and retrieve
	rd.Entries[0].SharesDst = math.LegacyNewDec(21)
	err = keeper.SetRedelegation(ctx, rd)
	require.NoError(err)

	resRed, err = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.NoError(err)
	require.Equal(rd, resRed)

	redelegations, err = keeper.GetRedelegationsFromSrcValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resRed)

	redelegations, err = keeper.GetRedelegations(ctx, addrDels[0], 5)
	require.NoError(err)
	require.Equal(1, len(redelegations))
	require.Equal(redelegations[0], resRed)

	// delete a record
	err = keeper.RemoveRedelegation(ctx, rd)
	require.NoError(err)
	_, err = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.ErrorIs(err, stakingtypes.ErrNoRedelegation)

	redelegations, err = keeper.GetRedelegations(ctx, addrDels[0], 5)
	require.NoError(err)
	require.Equal(0, len(redelegations))

	redelegations, err = keeper.GetAllRedelegations(ctx, addrDels[0], nil, nil)
	require.NoError(err)
	require.Equal(0, len(redelegations))
}

func (s *KeeperTestSuite) TestRedelegateToSameValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	_, addrVals := createValAddrs(1)
	valTokens := keeper.TokensFromConsensusPower(ctx, 10)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	require.True(validator.IsBonded())

	val0AccAddr := sdk.AccAddress(addrVals[0].Bytes())

	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	_, err := keeper.BeginRedelegation(ctx, val0AccAddr, addrVals[0], addrVals[0], math.LegacyNewDec(5))
	require.Error(err)
}

func (s *KeeperTestSuite) TestRedelegationMaxEntries() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	_, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	valTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	_ = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	val0AccAddr := sdk.AccAddress(addrVals[0].Bytes())
	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second validator
	validator2 := testutil.NewValidator(s.T(), addrVals[1], PKs[1])
	validator2, issuedShares = validator2.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator2 = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator2, true)
	require.Equal(stakingtypes.Bonded, validator2.Status)

	maxEntries, err := keeper.MaxEntries(ctx)
	require.NoError(err)

	// redelegations should pass
	var completionTime time.Time
	for i := uint32(0); i < maxEntries; i++ {
		var err error
		completionTime, err = keeper.BeginRedelegation(ctx, val0AccAddr, addrVals[0], addrVals[1], math.LegacyNewDec(1))
		require.NoError(err)
	}

	// an additional redelegation should fail due to max entries
	_, err = keeper.BeginRedelegation(ctx, val0AccAddr, addrVals[0], addrVals[1], math.LegacyNewDec(1))
	require.Error(err)

	// mature redelegations
	ctx = ctx.WithBlockTime(completionTime)
	_, err = keeper.CompleteRedelegation(ctx, val0AccAddr, addrVals[0], addrVals[1])
	require.NoError(err)

	// redelegation should work again
	_, err = keeper.BeginRedelegation(ctx, val0AccAddr, addrVals[0], addrVals[1], math.LegacyNewDec(1))
	require.NoError(err)
}

func (s *KeeperTestSuite) TestRedelegateSelfDelegation() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	valTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)

	val0AccAddr := sdk.AccAddress(addrVals[0])
	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second validator
	validator2 := testutil.NewValidator(s.T(), addrVals[1], PKs[1])
	validator2, issuedShares = validator2.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator2 = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator2, true)
	require.Equal(stakingtypes.Bonded, validator2.Status)

	// create a second delegation to validator 1
	delTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())
	stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)

	delegation := stakingtypes.NewDelegation(addrDels[0].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	_, err := keeper.BeginRedelegation(ctx, val0AccAddr, addrVals[0], addrVals[1], math.LegacyNewDecFromInt(delTokens))
	require.NoError(err)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 2)

	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(valTokens, validator.Tokens)
	require.Equal(stakingtypes.Unbonding, validator.Status)
}

func (s *KeeperTestSuite) TestRedelegateFromUnbondingValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	valTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	val0AccAddr := sdk.AccAddress(addrVals[0].Bytes())
	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second delegation to this validator
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	delTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())
	stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	delegation := stakingtypes.NewDelegation(addrDels[1].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	// create a second validator
	validator2 := testutil.NewValidator(s.T(), addrVals[1], PKs[1])
	validator2, issuedShares = validator2.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	_ = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator2, true)

	header := ctx.BlockHeader()
	blockHeight := int64(10)
	header.Height = blockHeight
	blockTime := time.Unix(333, 0)
	header.Time = blockTime
	ctx = ctx.WithBlockHeader(header)

	// unbond the all self-delegation to put validator in unbonding state
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	_, amount, err := keeper.Undelegate(ctx, val0AccAddr, addrVals[0], math.LegacyNewDecFromInt(delTokens))
	require.NoError(err)
	require.Equal(amount, delTokens)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 1)

	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(blockHeight, validator.UnbondingHeight)
	params, err := keeper.GetParams(ctx)
	require.NoError(err)
	require.True(blockTime.Add(params.UnbondingTime).Equal(validator.UnbondingTime))

	// change the context
	header = ctx.BlockHeader()
	blockHeight2 := int64(20)
	header.Height = blockHeight2
	blockTime2 := time.Unix(444, 0)
	header.Time = blockTime2
	ctx = ctx.WithBlockHeader(header)

	// unbond some of the other delegation's shares
	redelegateTokens := keeper.TokensFromConsensusPower(ctx, 6)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	_, err = keeper.BeginRedelegation(ctx, addrDels[1], addrVals[0], addrVals[1], math.LegacyNewDecFromInt(redelegateTokens))
	require.NoError(err)

	// retrieve the unbonding delegation
	ubd, err := keeper.GetRedelegation(ctx, addrDels[1], addrVals[0], addrVals[1])
	require.NoError(err)
	require.Len(ubd.Entries, 1)
	require.Equal(blockHeight, ubd.Entries[0].CreationHeight)
	require.True(blockTime.Add(params.UnbondingTime).Equal(ubd.Entries[0].CompletionTime))
}

func (s *KeeperTestSuite) TestRedelegateFromUnbondedValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	addrDels, addrVals := createValAddrs(2)

	// create a validator with a self-delegation
	validator := testutil.NewValidator(s.T(), addrVals[0], PKs[0])
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	valTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	val0AccAddr := sdk.AccAddress(addrVals[0].Bytes())
	selfDelegation := stakingtypes.NewDelegation(val0AccAddr.String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, selfDelegation))

	// create a second delegation to this validator
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	delTokens := keeper.TokensFromConsensusPower(ctx, 10)
	validator, issuedShares = validator.AddTokensFromDel(delTokens)
	require.Equal(delTokens, issuedShares.RoundInt())
	stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	delegation := stakingtypes.NewDelegation(addrDels[1].String(), addrVals[0].String(), issuedShares)
	require.NoError(keeper.SetDelegation(ctx, delegation))

	// create a second validator
	validator2 := testutil.NewValidator(s.T(), addrVals[1], PKs[1])
	validator2, issuedShares = validator2.AddTokensFromDel(valTokens)
	require.Equal(valTokens, issuedShares.RoundInt())
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validator2 = stakingkeeper.TestingUpdateValidator(keeper, ctx, validator2, true)
	require.Equal(stakingtypes.Bonded, validator2.Status)

	ctx = ctx.WithBlockHeight(10)
	ctx = ctx.WithBlockTime(time.Unix(333, 0))

	// unbond the all self-delegation to put validator in unbonding state
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	_, amount, err := keeper.Undelegate(ctx, val0AccAddr, addrVals[0], math.LegacyNewDecFromInt(delTokens))
	require.NoError(err)
	require.Equal(amount, delTokens)

	// end block
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 1)

	validator, err = keeper.GetValidator(ctx, addrVals[0])
	require.NoError(err)
	require.Equal(ctx.BlockHeight(), validator.UnbondingHeight)
	params, err := keeper.GetParams(ctx)
	require.NoError(err)
	require.True(ctx.BlockHeader().Time.Add(params.UnbondingTime).Equal(validator.UnbondingTime))

	// unbond the validator
	_, err = keeper.UnbondingToUnbonded(ctx, validator)
	require.NoError(err)

	// redelegate some of the delegation's shares
	redelegationTokens := keeper.TokensFromConsensusPower(ctx, 6)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	_, err = keeper.BeginRedelegation(ctx, addrDels[1], addrVals[0], addrVals[1], math.LegacyNewDecFromInt(redelegationTokens))
	require.NoError(err)

	// no red should have been found
	red, err := keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.ErrorIs(err, stakingtypes.ErrNoRedelegation, "%v", red)
}

func (s *KeeperTestSuite) TestUnbondingDelegationAddEntry() {
	require := s.Require()

	delAddrs, valAddrs := createValAddrs(1)

	delAddr := delAddrs[0]
	valAddr := valAddrs[0]
	creationHeight := int64(10)
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddr,
		valAddr,
		creationHeight,
		time.Unix(0, 0).UTC(),
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)
	var initialEntries []stakingtypes.UnbondingDelegationEntry
	initialEntries = append(initialEntries, ubd.Entries...)
	require.Len(initialEntries, 1)

	isNew := ubd.AddEntry(creationHeight, time.Unix(0, 0).UTC(), math.NewInt(5))
	require.False(isNew)
	require.Len(ubd.Entries, 1) // entry was merged
	require.NotEqual(initialEntries, ubd.Entries)
	require.Equal(creationHeight, ubd.Entries[0].CreationHeight)
	require.Equal(initialEntries[0].UnbondingId, ubd.Entries[0].UnbondingId) // unbondingID remains unchanged
	require.Equal(ubd.Entries[0].Balance, math.NewInt(15))                   // 10 from previous + 5 from merged

	newCreationHeight := int64(11)
	isNew = ubd.AddEntry(newCreationHeight, time.Unix(1, 0).UTC(), math.NewInt(5))
	require.True(isNew)
	require.Len(ubd.Entries, 2) // entry was appended
	require.NotEqual(initialEntries, ubd.Entries)
	require.Equal(creationHeight, ubd.Entries[0].CreationHeight)
	require.Equal(newCreationHeight, ubd.Entries[1].CreationHeight)
	require.Equal(ubd.Entries[0].Balance, math.NewInt(15))
	require.Equal(ubd.Entries[1].Balance, math.NewInt(5))
}

func (s *KeeperTestSuite) TestSetUnbondingDelegationEntry() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	delAddrs, valAddrs := createValAddrs(1)

	delAddr := delAddrs[0]
	valAddr := valAddrs[0]
	creationHeight := int64(0)
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddr,
		valAddr,
		creationHeight,
		time.Unix(0, 0).UTC(),
		math.NewInt(5),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)

	// set and retrieve a record
	require.NoError(keeper.SetUnbondingDelegation(ctx, ubd))
	resUnbond, err := keeper.GetUnbondingDelegation(ctx, delAddr, valAddr)
	require.NoError(err)
	require.Equal(ubd, resUnbond)

	initialEntries := ubd.Entries
	require.Len(initialEntries, 1)
	require.Equal(initialEntries[0].Balance, math.NewInt(5))
	require.Equal(initialEntries[0].UnbondingId, uint64(0)) // initial unbondingID

	// set unbonding delegation entry for existing creationHeight
	// entries are expected to be merged
	_, err = keeper.SetUnbondingDelegationEntry(
		ctx,
		delAddr,
		valAddr,
		creationHeight,
		time.Unix(0, 0).UTC(),
		math.NewInt(5),
	)
	require.NoError(err)
	resUnbonding, err := keeper.GetUnbondingDelegation(ctx, delAddr, valAddr)
	require.NoError(err)
	require.Len(resUnbonding.Entries, 1)
	require.NotEqual(initialEntries, resUnbonding.Entries)
	require.Equal(creationHeight, resUnbonding.Entries[0].CreationHeight)
	require.Equal(resUnbonding.Entries[0].Balance, math.NewInt(10)) // 5 from previous entry + 5 from merged entry

	// set unbonding delegation entry for newCreationHeight
	// new entry is expected to be appended to the existing entries
	newCreationHeight := int64(1)
	_, err = keeper.SetUnbondingDelegationEntry(
		ctx,
		delAddr,
		valAddr,
		newCreationHeight,
		time.Unix(1, 0).UTC(),
		math.NewInt(10),
	)
	require.NoError(err)
	resUnbonding, err = keeper.GetUnbondingDelegation(ctx, delAddr, valAddr)
	require.NoError(err)
	require.Len(resUnbonding.Entries, 2)
	require.NotEqual(initialEntries, resUnbonding.Entries)
	require.NotEqual(resUnbonding.Entries[0], resUnbonding.Entries[1])
	require.Equal(creationHeight, resUnbonding.Entries[0].CreationHeight)
	require.Equal(newCreationHeight, resUnbonding.Entries[1].CreationHeight)
}

func (s *KeeperTestSuite) TestInitUBDsCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	// add unbonding delegation directly to store
	delAddrs, valAddrs := createValAddrs(2)
	dvPair := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[0].String(),
		ValidatorAddress: valAddrs[0].String(),
	}
	t := blockTime
	require.NoError(keeper.SetUBDQueueStore(ctx, t, []stakingtypes.DVPair{dvPair}))

	// add another unbonding delegation directly to store
	dvPair1 := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[1].String(),
		ValidatorAddress: valAddrs[1].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.SetUBDQueueStore(ctx, t1, []stakingtypes.DVPair{dvPair1}))

	// init unbonding delegations cache should return the inserted unbonding delegations
	cache, err := keeper.InitUBDsCache(ctx)

	require.NoError(err)
	require.Equal(2, len(cache))
	require.Equal(dvPair.DelegatorAddress, cache[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(dvPair.ValidatorAddress, cache[sdk.FormatTimeString(t)][0].ValidatorAddress)
	require.Equal(dvPair1.DelegatorAddress, cache[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(dvPair1.ValidatorAddress, cache[sdk.FormatTimeString(t1)][0].ValidatorAddress)
}

func (s *KeeperTestSuite) TestGetAllUnbondingDelegations() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	delAddrs, valAddrs := createValAddrs(2)

	// insert unbonding delegation
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddrs[0],
		valAddrs[0],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)

	t := blockTime
	require.NoError(keeper.InsertUBDQueue(ctx, ubd, t))

	// add another unbonding delegation
	ubd1 := stakingtypes.NewUnbondingDelegation(
		delAddrs[1],
		valAddrs[1],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.InsertUBDQueue(ctx, ubd1, t1))

	// get all unbonding delegations should return the inserted unbonding delegations
	unbondingDelegations, err := keeper.GetUBDs(ctx)
	require.NoError(err)
	require.Equal(2, len(unbondingDelegations))
	require.Equal(ubd.DelegatorAddress, unbondingDelegations[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(ubd.ValidatorAddress, unbondingDelegations[sdk.FormatTimeString(t)][0].ValidatorAddress)
	require.Equal(ubd1.DelegatorAddress, unbondingDelegations[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(ubd1.ValidatorAddress, unbondingDelegations[sdk.FormatTimeString(t1)][0].ValidatorAddress)
}

func (s *KeeperTestSuite) TestGetUnbondingDelegationCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	// add unbonding delegation
	delAddrs, valAddrs := createValAddrs(2)
	dvPair := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[0].String(),
		ValidatorAddress: valAddrs[0].String(),
	}
	t := blockTime
	require.NoError(keeper.SetUBDQueueTimeSlice(ctx, t, []stakingtypes.DVPair{dvPair}))

	// add another unbonding delegation
	dvPair1 := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[1].String(),
		ValidatorAddress: valAddrs[1].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.SetUBDQueueTimeSlice(ctx, t1, []stakingtypes.DVPair{dvPair1}))

	// get unbonding delegations should return the inserted unbonding delegations
	cache := keeper.GetUnbondingDelegationCache(ctx)
	require.Equal(2, len(cache))
	require.Equal(dvPair.DelegatorAddress, cache[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(dvPair.ValidatorAddress, cache[sdk.FormatTimeString(t)][0].ValidatorAddress)
	require.Equal(dvPair1.DelegatorAddress, cache[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(dvPair1.ValidatorAddress, cache[sdk.FormatTimeString(t1)][0].ValidatorAddress)
}

func (s *KeeperTestSuite) TestSetUBDQueueCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	// add unbonding delegation
	delAddrs, valAddrs := createValAddrs(1)
	dvPair := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[0].String(),
		ValidatorAddress: valAddrs[0].String(),
	}
	t := blockTime
	require.NoError(keeper.SetUBDQueueCache(ctx, t, []stakingtypes.DVPair{dvPair}))

	// cache should be populated with unbonding validator
	require.Equal(1, len(keeper.GetUnbondingDelegationCache(ctx)))
	require.Equal(dvPair.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorAddress)
	require.Equal(dvPair.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][0].DelegatorAddress)
}

func (s *KeeperTestSuite) TestSetUBDQueueStore() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	iterator, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// no unbonding delegations in the queue initially
	require.Equal(0, count)

	// add unbonding delegation directly to store
	delAddrs, valAddrs := createValAddrs(2)
	dvPair := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[0].String(),
		ValidatorAddress: valAddrs[0].String(),
	}
	t := blockTime
	require.NoError(keeper.SetUBDQueueStore(ctx, t, []stakingtypes.DVPair{dvPair}))

	// add another unbonding delegation directly to store
	dvPair1 := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[1].String(),
		ValidatorAddress: valAddrs[1].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.SetUBDQueueStore(ctx, t1, []stakingtypes.DVPair{dvPair1}))

	iterator1, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator1.Close()
	count1 := 0
	for ; iterator1.Valid(); iterator1.Next() {
		count1++
	}

	// unbonding delegations should be retrieved
	require.Equal(2, count1)
}

func (s *KeeperTestSuite) TestInsertUBDQueue() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	iterator, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// no unbonding delegations in the queue initially
	require.Equal(0, count)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	delAddrs, valAddrs := createValAddrs(3)

	// insert unbonding delegation
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddrs[0],
		valAddrs[0],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)

	t := blockTime
	require.NoError(keeper.InsertUBDQueue(ctx, ubd, t))

	// insert another unbonding delegation
	ubd1 := stakingtypes.NewUnbondingDelegation(
		delAddrs[1],
		valAddrs[1],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)

	require.NoError(keeper.InsertUBDQueue(ctx, ubd1, t))

	iterator1, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator1.Close()
	count1 := 0
	for ; iterator1.Valid(); iterator1.Next() {
		count1++
	}

	// unbonding delegation should be retrieved
	// count 1 due to same unbonding time
	require.Equal(1, count1)

	// cache should be populated with unbonding validators
	require.Equal(1, len(keeper.GetUnbondingDelegationCache(ctx))) // length 1 due to same unbonding time
	require.Equal(ubd.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(ubd.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorAddress)
	require.Equal(ubd1.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][1].DelegatorAddress)
	require.Equal(ubd1.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][1].ValidatorAddress)

	// insert unbonding delegation with different unbonding time and height
	ubd2 := stakingtypes.NewUnbondingDelegation(
		delAddrs[2],
		valAddrs[2],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.InsertUBDQueue(ctx, ubd2, t1))

	iterator2, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator2.Close()
	count2 := 0
	for ; iterator2.Valid(); iterator2.Next() {
		count2++
	}

	// unbonding delegation should be retrieved
	require.Equal(2, count2)

	// cache should be populated with unbonding validators
	require.Equal(2, len(keeper.GetUnbondingDelegationCache(ctx)))
	require.Equal(ubd.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(ubd.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorAddress)
	require.Equal(ubd1.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][1].DelegatorAddress)
	require.Equal(ubd1.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t)][1].ValidatorAddress)
	require.Equal(ubd2.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(ubd2.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(t1)][0].ValidatorAddress)
}

func (s *KeeperTestSuite) TestDeleteMatureUBDsCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	// add unbonding delegation directly to cache
	delAddrs, valAddrs := createValAddrs(1)
	dvPair := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[0].String(),
		ValidatorAddress: valAddrs[0].String(),
	}
	require.NoError(keeper.SetUBDQueueCache(ctx, blockTime, []stakingtypes.DVPair{dvPair}))

	// cache should be populated with unbonding delegation
	require.Equal(1, len(keeper.GetUnbondingDelegationCache(ctx)))
	require.Equal(dvPair.DelegatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(blockTime)][0].DelegatorAddress)
	require.Equal(dvPair.ValidatorAddress, keeper.GetUnbondingDelegationCache(ctx)[sdk.FormatTimeString(blockTime)][0].ValidatorAddress)

	keeper.DeleteMatureUBDsCache(ctx, sdk.FormatTimeString(blockTime))

	// cache should also remove the removed unbonding delegation
	require.Equal(0, len(keeper.GetUnbondingValidatorsCache(ctx)))
}

func (s *KeeperTestSuite) TestDeleteMatureUBDsStore() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// add unbonding delegation directly to store
	delAddrs, valAddrs := createValAddrs(2)
	dvPair := stakingtypes.DVPair{
		DelegatorAddress: delAddrs[0].String(),
		ValidatorAddress: valAddrs[0].String(),
	}
	t := blockTime
	require.NoError(keeper.SetUBDQueueStore(ctx, t, []stakingtypes.DVPair{dvPair}))

	iterator, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}

	// unbonding delegation in the queue
	require.Equal(1, count)
	require.NoError(keeper.DeleteMatureUBDsStore(ctx, sdk.FormatTimeString(blockTime)))

	iterator, err = keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count = 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}

	// unbonding delegation should be removed
	require.Equal(0, count)
}

func (s *KeeperTestSuite) TestGetAndParseUnbondingDelegationTimeKey() {
	require := s.Require()

	blockTime := time.Now().UTC()
	key := stakingtypes.GetUnbondingDelegationTimeKey(blockTime)
	time, err := stakingtypes.ParseUnbondingDelegationTimeKey(key)
	require.NoError(err)
	require.Equal(blockTime, time)
}

func (s *KeeperTestSuite) TestDequeueAllMatureUBDQueue() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingDelegationCache(ctx))

	delAddrs, valAddrs := createValAddrs(2)

	// insert unbonding delegation - ready to unbond
	ubd := stakingtypes.NewUnbondingDelegation(
		delAddrs[0],
		valAddrs[0],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)

	t := blockTime
	require.NoError(keeper.InsertUBDQueue(ctx, ubd, t))

	// add another unbonding delegation - ready to unbond
	ubd1 := stakingtypes.NewUnbondingDelegation(
		delAddrs[1],
		valAddrs[1],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.InsertUBDQueue(ctx, ubd1, t1))

	// add another unbonding delegation - not ready to unbond
	ubd2 := stakingtypes.NewUnbondingDelegation(
		delAddrs[1],
		valAddrs[1],
		blockHeight,
		blockTime,
		math.NewInt(10),
		address.NewBech32Codec("cosmosvaloper"),
		address.NewBech32Codec("cosmos"),
	)
	t2 := blockTime.Add(1 * time.Minute)
	require.NoError(keeper.InsertUBDQueue(ctx, ubd2, t2))

	// cache should be populated with unbonding delegations
	require.Equal(3, len(keeper.GetUnbondingDelegationCache(ctx)))

	matureUnbonds, err := keeper.DequeueAllMatureUBDQueue(ctx, blockTime)

	require.NoError(err)
	require.Equal(2, len(matureUnbonds))

	// all ready to unbond unbonding delegations should be removed
	iterator, err := keeper.UBDQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	require.Equal(1, count)

	// cache should be populated with the pending to unbond unbonding delegations
	require.Equal(1, len(keeper.GetUnbondingDelegationCache(ctx)))
}

func (s *KeeperTestSuite) TestInitRedelegationsCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	// add redelegation directly to store
	delAddrs, valAddrs := createValAddrs(2)
	dvvTriplet := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}
	t := blockTime
	require.NoError(keeper.SetRedelegationQueueStore(ctx, t, []stakingtypes.DVVTriplet{dvvTriplet}))

	// add another redelegation directly to store
	dvvTriplet1 := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[1].String(),
		ValidatorSrcAddress: valAddrs[1].String(),
		ValidatorDstAddress: valAddrs[0].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.SetRedelegationQueueStore(ctx, t1, []stakingtypes.DVVTriplet{dvvTriplet1}))

	// init redelegations cache should return the inserted redelegations
	cache, err := keeper.InitRedelegationsCache(ctx)

	require.NoError(err)
	require.Equal(2, len(cache))
	require.Equal(dvvTriplet.DelegatorAddress, cache[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(dvvTriplet.ValidatorSrcAddress, cache[sdk.FormatTimeString(t)][0].ValidatorSrcAddress)
	require.Equal(dvvTriplet1.DelegatorAddress, cache[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(dvvTriplet1.ValidatorSrcAddress, cache[sdk.FormatTimeString(t1)][0].ValidatorSrcAddress)
}

func (s *KeeperTestSuite) TestGetPendingRedelegations() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	delAddrs, valAddrs := createValAddrs(2)

	// insert redelegation
	red := stakingtypes.Redelegation{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}

	t := blockTime
	require.NoError(keeper.InsertRedelegationQueue(ctx, red, t))

	// add another redelegation
	red1 := stakingtypes.Redelegation{
		DelegatorAddress:    delAddrs[1].String(),
		ValidatorSrcAddress: valAddrs[1].String(),
		ValidatorDstAddress: valAddrs[0].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.InsertRedelegationQueue(ctx, red1, t1))

	// get all redelegations should return the inserted redelegations
	redelegations, err := keeper.GetPendingRedelegations(ctx)
	require.NoError(err)
	require.Equal(2, len(redelegations))
	require.Equal(red.DelegatorAddress, redelegations[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(red.ValidatorSrcAddress, redelegations[sdk.FormatTimeString(t)][0].ValidatorSrcAddress)
	require.Equal(red.ValidatorDstAddress, redelegations[sdk.FormatTimeString(t)][0].ValidatorDstAddress)
	require.Equal(red1.DelegatorAddress, redelegations[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(red1.ValidatorSrcAddress, redelegations[sdk.FormatTimeString(t1)][0].ValidatorSrcAddress)
	require.Equal(red1.ValidatorDstAddress, redelegations[sdk.FormatTimeString(t1)][0].ValidatorDstAddress)
}

func (s *KeeperTestSuite) TestGetRedelegationCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	// add redelegation
	delAddrs, valAddrs := createValAddrs(2)
	dvvTriplet := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}
	t := blockTime
	require.NoError(keeper.SetRedelegationQueueTimeSlice(ctx, t, []stakingtypes.DVVTriplet{dvvTriplet}))

	// add another redelegation
	dvvTriplet1 := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[1].String(),
		ValidatorSrcAddress: valAddrs[1].String(),
		ValidatorDstAddress: valAddrs[0].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.SetRedelegationQueueTimeSlice(ctx, t1, []stakingtypes.DVVTriplet{dvvTriplet1}))

	// get redelegations should return the inserted redelegations
	cache := keeper.GetRedelegationCache(ctx)
	require.Equal(2, len(cache))
	require.Equal(dvvTriplet.DelegatorAddress, cache[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(dvvTriplet.ValidatorSrcAddress, cache[sdk.FormatTimeString(t)][0].ValidatorSrcAddress)
	require.Equal(dvvTriplet1.DelegatorAddress, cache[sdk.FormatTimeString(t1)][0].DelegatorAddress)
	require.Equal(dvvTriplet1.ValidatorSrcAddress, cache[sdk.FormatTimeString(t1)][0].ValidatorSrcAddress)
}

func (s *KeeperTestSuite) TestSetRedelegationQueueCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	// add redelegation
	delAddrs, valAddrs := createValAddrs(2)
	dvvTriplet := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}
	t := blockTime
	require.NoError(keeper.SetRedelegationQueueCache(ctx, t, []stakingtypes.DVVTriplet{dvvTriplet}))

	// cache should be populated with unbonding validator
	require.Equal(1, len(keeper.GetRedelegationCache(ctx)))
	require.Equal(dvvTriplet.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorSrcAddress)
	require.Equal(dvvTriplet.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorDstAddress)
	require.Equal(dvvTriplet.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].DelegatorAddress)
}

func (s *KeeperTestSuite) TestSetRedelegationQueueStore() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	iterator, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// no redelegations in the queue initially
	require.Equal(0, count)

	// add redelegation directly to store
	delAddrs, valAddrs := createValAddrs(2)
	dvvTriplet := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}
	t := blockTime
	require.NoError(keeper.SetRedelegationQueueStore(ctx, t, []stakingtypes.DVVTriplet{dvvTriplet}))

	// add another redelegation directly to store
	dvvTriplet1 := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[1].String(),
		ValidatorSrcAddress: valAddrs[1].String(),
		ValidatorDstAddress: valAddrs[0].String(),
	}
	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.SetRedelegationQueueStore(ctx, t1, []stakingtypes.DVVTriplet{dvvTriplet1}))

	iterator1, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator1.Close()
	count1 := 0
	for ; iterator1.Valid(); iterator1.Next() {
		count1++
	}

	// redelegations should be retrieved
	require.Equal(2, count1)
}

func (s *KeeperTestSuite) TestInsertRedelegationQueue() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	iterator, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// no redelegations in the queue initially
	require.Equal(0, count)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	delAddrs, valAddrs := createValAddrs(3)

	// insert redelegation
	red := stakingtypes.NewRedelegation(delAddrs[0], valAddrs[0], valAddrs[1], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))

	t := blockTime
	require.NoError(keeper.InsertRedelegationQueue(ctx, red, t))

	// insert another redelegation
	red1 := stakingtypes.NewRedelegation(delAddrs[1], valAddrs[1], valAddrs[0], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))

	require.NoError(keeper.InsertRedelegationQueue(ctx, red1, t))

	iterator1, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator1.Close()
	count1 := 0
	for ; iterator1.Valid(); iterator1.Next() {
		count1++
	}

	// redelegation should be retrieved
	// count 1 due to same redelegation time
	require.Equal(1, count1)

	// cache should be populated with redelegations
	require.Equal(1, len(keeper.GetRedelegationCache(ctx))) // length 1 due to same redelegation time
	require.Equal(red.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(red.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorSrcAddress)
	require.Equal(red.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorDstAddress)
	require.Equal(red1.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][1].DelegatorAddress)
	require.Equal(red1.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][1].ValidatorSrcAddress)
	require.Equal(red1.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][1].ValidatorDstAddress)

	// insert another redelegation with different redelegation time and height
	red2 := stakingtypes.NewRedelegation(delAddrs[2], valAddrs[2], valAddrs[0], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))
	t2 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.InsertRedelegationQueue(ctx, red2, t2))

	iterator2, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator2.Close()
	count2 := 0
	for ; iterator2.Valid(); iterator2.Next() {
		count2++
	}

	// redelegation should be retrieved
	require.Equal(2, count2)

	// cache should be populated with redelegations
	require.Equal(2, len(keeper.GetRedelegationCache(ctx)))
	require.Equal(red.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].DelegatorAddress)
	require.Equal(red.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorSrcAddress)
	require.Equal(red.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][0].ValidatorDstAddress)
	require.Equal(red1.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][1].DelegatorAddress)
	require.Equal(red1.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][1].ValidatorSrcAddress)
	require.Equal(red1.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t)][1].ValidatorDstAddress)
	require.Equal(red2.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t2)][0].DelegatorAddress)
	require.Equal(red2.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t2)][0].ValidatorSrcAddress)
	require.Equal(red2.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(t2)][0].ValidatorDstAddress)
}

func (s *KeeperTestSuite) TestDeleteMatureRedelegationsCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	// add redelegation directly to cache
	delAddrs, valAddrs := createValAddrs(2)
	dvvTriplet := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}
	require.NoError(keeper.SetRedelegationQueueCache(ctx, blockTime, []stakingtypes.DVVTriplet{dvvTriplet}))

	// cache should be populated with unbonding delegation
	require.Equal(1, len(keeper.GetRedelegationCache(ctx)))
	require.Equal(dvvTriplet.DelegatorAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(blockTime)][0].DelegatorAddress)
	require.Equal(dvvTriplet.ValidatorSrcAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(blockTime)][0].ValidatorSrcAddress)
	require.Equal(dvvTriplet.ValidatorDstAddress, keeper.GetRedelegationCache(ctx)[sdk.FormatTimeString(blockTime)][0].ValidatorDstAddress)

	keeper.DeleteMatureRedelegationsCache(ctx, sdk.FormatTimeString(blockTime))

	// cache should also remove the removed unbonding delegation
	require.Equal(0, len(keeper.GetRedelegationCache(ctx)))
}

func (s *KeeperTestSuite) TestDeleteMatureRedelegationsStore() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// add redelegation directly to store
	delAddrs, valAddrs := createValAddrs(2)
	dvvTriplet := stakingtypes.DVVTriplet{
		DelegatorAddress:    delAddrs[0].String(),
		ValidatorSrcAddress: valAddrs[0].String(),
		ValidatorDstAddress: valAddrs[1].String(),
	}
	t := blockTime
	require.NoError(keeper.SetRedelegationQueueStore(ctx, t, []stakingtypes.DVVTriplet{dvvTriplet}))

	iterator, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}

	// redelegation in the queue
	require.Equal(1, count)
	require.NoError(keeper.DeleteMatureRedelegationsStore(ctx, sdk.FormatTimeString(blockTime)))

	iterator, err = keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count = 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}

	// redelegation should be removed
	require.Equal(0, count)
}

func (s *KeeperTestSuite) TestGetAndParseRedelegationTimeKey() {
	require := s.Require()

	blockTime := time.Now().UTC()
	key := stakingtypes.GetRedelegationTimeKey(blockTime)
	time, err := stakingtypes.ParseRedelegationTimeKey(key)
	require.NoError(err)
	require.Equal(blockTime, time)
}

func (s *KeeperTestSuite) TestDequeueAllMatureRedelegationQueue() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetRedelegationCache(ctx))

	delAddrs, valAddrs := createValAddrs(3)

	// insert redelegation
	red := stakingtypes.NewRedelegation(delAddrs[0], valAddrs[0], valAddrs[1], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))

	t := blockTime
	require.NoError(keeper.InsertRedelegationQueue(ctx, red, t))

	// insert another redelegation
	red1 := stakingtypes.NewRedelegation(delAddrs[1], valAddrs[1], valAddrs[0], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))

	t1 := blockTime.Add(-1 * time.Minute)
	require.NoError(keeper.InsertRedelegationQueue(ctx, red1, t1))

	// insert another redelegation - not ready to redelegate
	red2 := stakingtypes.NewRedelegation(delAddrs[2], valAddrs[2], valAddrs[0], 0,
		time.Unix(0, 0), math.NewInt(5),
		math.LegacyNewDec(5), address.NewBech32Codec("cosmosvaloper"), address.NewBech32Codec("cosmos"))
	t2 := blockTime.Add(1 * time.Minute)
	require.NoError(keeper.InsertRedelegationQueue(ctx, red2, t2))

	// cache should be populated with redelegations
	require.Equal(3, len(keeper.GetRedelegationCache(ctx)))

	matureRedelegations, err := keeper.DequeueAllMatureRedelegationQueue(ctx, blockTime)

	require.NoError(err)
	require.Equal(2, len(matureRedelegations))

	// all ready to redelegate redelegations should be removed
	iterator, err := keeper.RedelegationQueueIterator(ctx)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	require.Equal(1, count)

	// cache should be populated with the pending to redelegate redelegations
	require.Equal(1, len(keeper.GetRedelegationCache(ctx)))
}

func (s *KeeperTestSuite) TestSortRedelegationQueueKeysByAscendingOrder() {
	require := s.Require()

	currentTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	oneHourLater := currentTime.Add(1 * time.Hour)
	oneHourBefore := currentTime.Add(-1 * time.Hour)

	keys := []string{
		sdk.FormatTimeString(oneHourLater),
		sdk.FormatTimeString(oneHourBefore),
		sdk.FormatTimeString(currentTime),
	}

	stakingtypes.SortTimestampsByAscendingOrder(keys)

	// Verify sorting is correct - should be sorted by timestamp ascending order
	for i := 0; i < len(keys)-1; i++ {
		t1, err := sdk.ParseTime(keys[i])
		require.NoError(err)
		t2, err := sdk.ParseTime(keys[i+1])
		require.NoError(err)

		// Current entry should be before or equal to next entry
		require.True(t1.Before(t2) || t1.Equal(t2), "timestamps should be in ascending order")

	}

	firstTime, err := sdk.ParseTime(keys[0])
	require.NoError(err)
	require.Equal(oneHourBefore, firstTime)

	lastTime, err := sdk.ParseTime(keys[len(keys)-1])
	require.NoError(err)
	require.Equal(oneHourLater, lastTime)
}
