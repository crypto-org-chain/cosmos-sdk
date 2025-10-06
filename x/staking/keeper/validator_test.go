package keeper_test

import (
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/golang/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (s *KeeperTestSuite) applyValidatorSetUpdates(ctx sdk.Context, keeper *stakingkeeper.Keeper, expectedUpdatesLen int) []abci.ValidatorUpdate {
	updates, err := keeper.ApplyAndReturnValidatorSetUpdates(ctx)
	s.Require().NoError(err)
	if expectedUpdatesLen >= 0 {
		s.Require().Equal(expectedUpdatesLen, len(updates), "%v", updates)
	}
	return updates
}

func (s *KeeperTestSuite) TestValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	valTokens := keeper.TokensFromConsensusPower(ctx, 10)

	// test how the validator is set from a purely unbonbed pool
	validator := testutil.NewValidator(s.T(), valAddr, valPubKey)
	validator, _ = validator.AddTokensFromDel(valTokens)
	require.Equal(stakingtypes.Unbonded, validator.Status)
	require.Equal(valTokens, validator.Tokens)
	require.Equal(valTokens, validator.DelegatorShares.RoundInt())
	require.NoError(keeper.SetValidator(ctx, validator))
	require.NoError(keeper.SetValidatorByPowerIndex(ctx, validator))
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validator))

	// ensure update
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	updates := s.applyValidatorSetUpdates(ctx, keeper, 1)
	validator, err := keeper.GetValidator(ctx, valAddr)
	require.NoError(err)
	require.Equal(validator.ABCIValidatorUpdate(keeper.PowerReduction(ctx)), updates[0])

	// after the save the validator should be bonded
	require.Equal(stakingtypes.Bonded, validator.Status)
	require.Equal(valTokens, validator.Tokens)
	require.Equal(valTokens, validator.DelegatorShares.RoundInt())

	// check each store for being saved
	consAddr, err := validator.GetConsAddr()
	require.NoError(err)
	resVal, err := keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.NoError(err)
	require.True(validator.MinEqual(&resVal))

	resVals, err := keeper.GetLastValidators(ctx)
	require.NoError(err)
	require.Equal(1, len(resVals))
	require.True(validator.MinEqual(&resVals[0]))

	resVals, err = keeper.GetBondedValidatorsByPower(ctx)
	require.NoError(err)
	require.Equal(1, len(resVals))
	require.True(validator.MinEqual(&resVals[0]))

	allVals, err := keeper.GetAllValidators(ctx)
	require.NoError(err)
	require.Equal(1, len(allVals))

	// check the last validator power
	power := int64(100)
	require.NoError(keeper.SetLastValidatorPower(ctx, valAddr, power))
	resPower, err := keeper.GetLastValidatorPower(ctx, valAddr)
	require.NoError(err)
	require.Equal(power, resPower)
	require.NoError(keeper.DeleteLastValidatorPower(ctx, valAddr))
	resPower, err = keeper.GetLastValidatorPower(ctx, valAddr)
	require.NoError(err)
	require.Equal(int64(0), resPower)
}

// This function tests UpdateValidator, GetValidator, GetLastValidators, RemoveValidator
func (s *KeeperTestSuite) TestValidatorBasics() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	// construct the validators
	var validators [3]stakingtypes.Validator
	powers := []int64{9, 8, 7}
	for i, power := range powers {
		validators[i] = testutil.NewValidator(s.T(), sdk.ValAddress(PKs[i].Address().Bytes()), PKs[i])
		validators[i].Status = stakingtypes.Unbonded
		validators[i].Tokens = math.ZeroInt()
		tokens := keeper.TokensFromConsensusPower(ctx, power)

		validators[i], _ = validators[i].AddTokensFromDel(tokens)
	}

	require.Equal(keeper.TokensFromConsensusPower(ctx, 9), validators[0].Tokens)
	require.Equal(keeper.TokensFromConsensusPower(ctx, 8), validators[1].Tokens)
	require.Equal(keeper.TokensFromConsensusPower(ctx, 7), validators[2].Tokens)

	// check the empty keeper first
	_, err := keeper.GetValidator(ctx, sdk.ValAddress(PKs[0].Address().Bytes()))
	require.ErrorIs(err, stakingtypes.ErrNoValidatorFound)
	resVals, err := keeper.GetLastValidators(ctx)
	require.NoError(err)
	require.Zero(len(resVals))

	resVals, err = keeper.GetValidators(ctx, 2)
	require.NoError(err)
	require.Len(resVals, 0)

	// set and retrieve a record
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validators[0] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[0], true)
	require.NoError(keeper.SetValidatorByConsAddr(ctx, validators[0]))
	resVal, err := keeper.GetValidator(ctx, sdk.ValAddress(PKs[0].Address().Bytes()))
	require.NoError(err)
	require.True(validators[0].MinEqual(&resVal))

	// retrieve from consensus
	resVal, err = keeper.GetValidatorByConsAddr(ctx, sdk.ConsAddress(PKs[0].Address()))
	require.NoError(err)
	require.True(validators[0].MinEqual(&resVal))
	resVal, err = keeper.GetValidatorByConsAddr(ctx, sdk.GetConsAddress(PKs[0]))
	require.NoError(err)
	require.True(validators[0].MinEqual(&resVal))

	resVals, err = keeper.GetLastValidators(ctx)
	require.NoError(err)
	require.Equal(1, len(resVals))
	require.True(validators[0].MinEqual(&resVals[0]))
	require.Equal(stakingtypes.Bonded, validators[0].Status)
	require.True(keeper.TokensFromConsensusPower(ctx, 9).Equal(validators[0].BondedTokens()))

	// modify a records, save, and retrieve
	validators[0].Status = stakingtypes.Bonded
	validators[0].Tokens = keeper.TokensFromConsensusPower(ctx, 10)
	validators[0].DelegatorShares = math.LegacyNewDecFromInt(validators[0].Tokens)
	validators[0] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[0], true)
	resVal, err = keeper.GetValidator(ctx, sdk.ValAddress(PKs[0].Address().Bytes()))
	require.NoError(err)
	require.True(validators[0].MinEqual(&resVal))

	resVals, err = keeper.GetLastValidators(ctx)
	require.NoError(err)
	require.Equal(1, len(resVals))
	require.True(validators[0].MinEqual(&resVals[0]))

	// add other validators
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validators[1] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[1], true)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validators[2] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[2], true)
	resVal, err = keeper.GetValidator(ctx, sdk.ValAddress(PKs[1].Address().Bytes()))
	require.NoError(err)
	require.True(validators[1].MinEqual(&resVal))
	resVal, err = keeper.GetValidator(ctx, sdk.ValAddress(PKs[2].Address().Bytes()))
	require.NoError(err)
	require.True(validators[2].MinEqual(&resVal))

	resVals, err = keeper.GetLastValidators(ctx)
	require.NoError(err)
	require.Equal(3, len(resVals))

	// remove a record

	bz, err := keeper.ValidatorAddressCodec().StringToBytes(validators[1].GetOperator())
	require.NoError(err)

	// shouldn't be able to remove if status is not unbonded
	require.EqualError(keeper.RemoveValidator(ctx, bz), "cannot call RemoveValidator on bonded or unbonding validators: failed to remove validator")

	// shouldn't be able to remove if there are still tokens left
	validators[1].Status = stakingtypes.Unbonded
	require.NoError(keeper.SetValidator(ctx, validators[1]))
	require.EqualError(keeper.RemoveValidator(ctx, bz), "attempting to remove a validator which still contains tokens: failed to remove validator")

	validators[1].Tokens = math.ZeroInt()                    // ...remove all tokens
	require.NoError(keeper.SetValidator(ctx, validators[1])) // ...set the validator
	require.NoError(keeper.RemoveValidator(ctx, bz))         // Now it can be removed.
	_, err = keeper.GetValidator(ctx, sdk.ValAddress(PKs[1].Address().Bytes()))
	require.ErrorIs(err, stakingtypes.ErrNoValidatorFound)
}

func (s *KeeperTestSuite) TestUpdateValidatorByPowerIndex() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	valTokens := keeper.TokensFromConsensusPower(ctx, 100)

	// add a validator
	validator := testutil.NewValidator(s.T(), valAddr, PKs[0])
	validator, delSharesCreated := validator.AddTokensFromDel(valTokens)
	require.Equal(stakingtypes.Unbonded, validator.Status)
	require.Equal(valTokens, validator.Tokens)

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true)
	validator, err := keeper.GetValidator(ctx, valAddr)
	require.NoError(err)
	require.Equal(valTokens, validator.Tokens)

	power := stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.PowerReduction(ctx), keeper.ValidatorAddressCodec())
	require.True(stakingkeeper.ValidatorByPowerIndexExists(ctx, keeper, power))

	// burn half the delegator shares
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	validator, burned := validator.RemoveDelShares(delSharesCreated.Quo(math.LegacyNewDec(2)))
	require.Equal(keeper.TokensFromConsensusPower(ctx, 50), burned)
	stakingkeeper.TestingUpdateValidator(keeper, ctx, validator, true) // update the validator, possibly kicking it out
	require.False(stakingkeeper.ValidatorByPowerIndexExists(ctx, keeper, power))

	validator, err = keeper.GetValidator(ctx, valAddr)
	require.NoError(err)

	power = stakingtypes.GetValidatorsByPowerIndexKey(validator, keeper.PowerReduction(ctx), keeper.ValidatorAddressCodec())
	require.True(stakingkeeper.ValidatorByPowerIndexExists(ctx, keeper, power))

	// set new validator by power index
	require.NoError(keeper.DeleteValidatorByPowerIndex(ctx, validator))
	require.False(stakingkeeper.ValidatorByPowerIndexExists(ctx, keeper, power))
	require.NoError(keeper.SetNewValidatorByPowerIndex(ctx, validator))
	require.True(stakingkeeper.ValidatorByPowerIndexExists(ctx, keeper, power))
}

func (s *KeeperTestSuite) TestApplyAndReturnValidatorSetUpdatesPowerDecrease() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	powers := []int64{100, 100}
	var validators [2]stakingtypes.Validator

	for i, power := range powers {
		validators[i] = testutil.NewValidator(s.T(), sdk.ValAddress(PKs[i].Address().Bytes()), PKs[i])
		tokens := keeper.TokensFromConsensusPower(ctx, power)
		validators[i], _ = validators[i].AddTokensFromDel(tokens)

	}

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validators[0] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[0], false)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	validators[1] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[1], false)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), stakingtypes.NotBondedPoolName, stakingtypes.BondedPoolName, gomock.Any())
	s.applyValidatorSetUpdates(ctx, keeper, 2)

	// check initial power
	require.Equal(int64(100), validators[0].GetConsensusPower(keeper.PowerReduction(ctx)))
	require.Equal(int64(100), validators[1].GetConsensusPower(keeper.PowerReduction(ctx)))

	// test multiple value change
	// tendermintUpdate set: {c1, c3} -> {c1', c3'}
	delTokens1 := keeper.TokensFromConsensusPower(ctx, 20)
	delTokens2 := keeper.TokensFromConsensusPower(ctx, 30)
	validators[0], _ = validators[0].RemoveDelShares(math.LegacyNewDecFromInt(delTokens1))
	validators[1], _ = validators[1].RemoveDelShares(math.LegacyNewDecFromInt(delTokens2))
	validators[0] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[0], false)
	validators[1] = stakingkeeper.TestingUpdateValidator(keeper, ctx, validators[1], false)

	// power has changed
	require.Equal(int64(80), validators[0].GetConsensusPower(keeper.PowerReduction(ctx)))
	require.Equal(int64(70), validators[1].GetConsensusPower(keeper.PowerReduction(ctx)))

	// CometBFT updates should reflect power change
	updates := s.applyValidatorSetUpdates(ctx, keeper, 2)
	require.Equal(validators[0].ABCIValidatorUpdate(keeper.PowerReduction(ctx)), updates[0])
	require.Equal(validators[1].ABCIValidatorUpdate(keeper.PowerReduction(ctx)), updates[1])
}

func (s *KeeperTestSuite) TestUpdateValidatorCommission() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	// Set MinCommissionRate to 0.05
	params, err := keeper.GetParams(ctx)
	require.NoError(err)
	params.MinCommissionRate = math.LegacyNewDecWithPrec(5, 2)
	require.NoError(keeper.SetParams(ctx, params))

	commission1 := stakingtypes.NewCommissionWithTime(
		math.LegacyNewDecWithPrec(1, 1), math.LegacyNewDecWithPrec(3, 1),
		math.LegacyNewDecWithPrec(1, 1), time.Now().UTC().Add(time.Duration(-1)*time.Hour),
	)
	commission2 := stakingtypes.NewCommission(math.LegacyNewDecWithPrec(1, 1), math.LegacyNewDecWithPrec(3, 1), math.LegacyNewDecWithPrec(1, 1))

	val1 := testutil.NewValidator(s.T(), sdk.ValAddress(PKs[0].Address().Bytes()), PKs[0])
	val2 := testutil.NewValidator(s.T(), sdk.ValAddress(PKs[1].Address().Bytes()), PKs[1])

	val1, _ = val1.SetInitialCommission(commission1)
	val2, _ = val2.SetInitialCommission(commission2)

	require.NoError(keeper.SetValidator(ctx, val1))
	require.NoError(keeper.SetValidator(ctx, val2))

	testCases := []struct {
		validator   stakingtypes.Validator
		newRate     math.LegacyDec
		expectedErr bool
	}{
		{val1, math.LegacyZeroDec(), true},
		{val2, math.LegacyNewDecWithPrec(-1, 1), true},
		{val2, math.LegacyNewDecWithPrec(4, 1), true},
		{val2, math.LegacyNewDecWithPrec(3, 1), true},
		{val2, math.LegacyNewDecWithPrec(1, 2), true},
		{val2, math.LegacyNewDecWithPrec(2, 1), false},
	}

	for i, tc := range testCases {
		commission, err := keeper.UpdateValidatorCommission(ctx, tc.validator, tc.newRate)

		if tc.expectedErr {
			require.Error(err, "expected error for test case #%d with rate: %s", i, tc.newRate)
		} else {
			require.NoError(err,
				"unexpected error for test case #%d with rate: %s", i, tc.newRate,
			)

			tc.validator.Commission = commission
			err = keeper.SetValidator(ctx, tc.validator)
			require.NoError(err)

			bz, err := keeper.ValidatorAddressCodec().StringToBytes(tc.validator.GetOperator())
			require.NoError(err)

			val, err := keeper.GetValidator(ctx, bz)
			require.NoError(err,
				"expected to find validator for test case #%d with rate: %s", i, tc.newRate,
			)

			require.Equal(tc.newRate, val.Commission.Rate,
				"expected new validator commission rate for test case #%d with rate: %s", i, tc.newRate,
			)
			require.Equal(ctx.BlockHeader().Time, val.Commission.UpdateTime,
				"expected new validator commission update time for test case #%d with rate: %s", i, tc.newRate,
			)
		}
	}
}

func (s *KeeperTestSuite) TestValidatorToken() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	addTokens := keeper.TokensFromConsensusPower(ctx, 10)
	delTokens := keeper.TokensFromConsensusPower(ctx, 5)

	validator := testutil.NewValidator(s.T(), valAddr, valPubKey)
	validator, _, err := keeper.AddValidatorTokensAndShares(ctx, validator, addTokens)
	require.NoError(err)
	require.Equal(addTokens, validator.Tokens)
	validator, _ = keeper.GetValidator(ctx, valAddr)
	require.Equal(math.LegacyNewDecFromInt(addTokens), validator.DelegatorShares)

	_, _, err = keeper.RemoveValidatorTokensAndShares(ctx, validator, math.LegacyNewDecFromInt(delTokens))
	require.NoError(err)
	validator, _ = keeper.GetValidator(ctx, valAddr)
	require.Equal(delTokens, validator.Tokens)
	require.True(validator.DelegatorShares.Equal(math.LegacyNewDecFromInt(delTokens)))

	_, err = keeper.RemoveValidatorTokens(ctx, validator, delTokens)
	require.NoError(err)
	validator, _ = keeper.GetValidator(ctx, valAddr)
	require.True(validator.Tokens.IsZero())
}

func (s *KeeperTestSuite) TestUnbondingValidator() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	validator := testutil.NewValidator(s.T(), valAddr, valPubKey)
	addTokens := keeper.TokensFromConsensusPower(ctx, 10)

	// set unbonding validator
	endTime := time.Now()
	endHeight := ctx.BlockHeight() + 10
	require.NoError(keeper.SetUnbondingValidatorsQueue(ctx, endTime, endHeight, []string{valAddr.String()}))

	resVals, err := keeper.GetUnbondingValidators(ctx, endTime, endHeight)
	require.NoError(err)
	require.Equal(1, len(resVals))
	require.Equal(valAddr.String(), resVals[0])

	// add another unbonding validator
	valAddr1 := sdk.ValAddress(PKs[1].Address().Bytes())
	validator1 := testutil.NewValidator(s.T(), valAddr1, PKs[1])
	validator1.UnbondingHeight = endHeight
	validator1.UnbondingTime = endTime
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, validator1))

	resVals, err = keeper.GetUnbondingValidators(ctx, endTime, endHeight)
	require.NoError(err)
	require.Equal(2, len(resVals))

	// delete unbonding validator from the queue
	require.NoError(keeper.DeleteValidatorQueue(ctx, validator1))
	resVals, err = keeper.GetUnbondingValidators(ctx, endTime, endHeight)
	require.NoError(err)
	require.Equal(1, len(resVals))
	require.Equal(valAddr.String(), resVals[0])

	// check unbonding mature validators
	ctx = ctx.WithBlockHeight(endHeight).WithBlockTime(endTime)
	err = keeper.UnbondAllMatureValidators(ctx)
	require.EqualError(err, "validator in the unbonding queue was not found: validator does not exist")

	require.NoError(keeper.SetValidator(ctx, validator))
	ctx = ctx.WithBlockHeight(endHeight).WithBlockTime(endTime)

	err = keeper.UnbondAllMatureValidators(ctx)
	require.EqualError(err, "unexpected validator in unbonding queue; status was not unbonding")

	validator.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator))
	require.NoError(keeper.UnbondAllMatureValidators(ctx))
	validator, err = keeper.GetValidator(ctx, valAddr)
	require.ErrorIs(err, stakingtypes.ErrNoValidatorFound)

	require.NoError(keeper.SetUnbondingValidatorsQueue(ctx, endTime, endHeight, []string{valAddr.String()}))
	validator = testutil.NewValidator(s.T(), valAddr, valPubKey)
	validator, _ = validator.AddTokensFromDel(addTokens)
	validator.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator))
	require.NoError(keeper.UnbondAllMatureValidators(ctx))
	validator, err = keeper.GetValidator(ctx, valAddr)
	require.NoError(err)
	require.Equal(stakingtypes.Unbonded, validator.Status)
}

func (s *KeeperTestSuite) TestInitUnbondingValidatorsCache() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))

	// add ready to unbond validator directly to store
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.SetUnbondingValidatorQueueStore(ctx, val.UnbondingTime, val.UnbondingHeight, []string{val.GetOperator()}))

	// add another unbonding validator directly to store
	valAddr1 := sdk.ValAddress(PKs[1].Address().Bytes())
	validator1 := testutil.NewValidator(s.T(), valAddr1, PKs[1])
	valUnbondingHeight1 := blockHeight - 10
	valUnbondingTime1 := blockTime.Add(-1 * time.Minute)
	validator1.UnbondingHeight = valUnbondingHeight1
	validator1.UnbondingTime = valUnbondingTime1
	validator1.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator1))
	require.NoError(keeper.SetUnbondingValidatorQueueStore(ctx, validator1.UnbondingTime, validator1.UnbondingHeight, []string{validator1.GetOperator()}))

	// init unbonding validators cache should return the inserted validators
	unbondingValidators, err := keeper.InitUnbondingValidatorsCache(ctx)
	require.NoError(err)
	require.Equal(2, len(unbondingValidators))
	require.Equal(val.GetOperator(), unbondingValidators[stakingtypes.GetCacheValidatorQueueKey(val.UnbondingTime, val.UnbondingHeight)][0])
	require.Equal(validator1.GetOperator(), unbondingValidators[stakingtypes.GetCacheValidatorQueueKey(validator1.UnbondingTime, validator1.UnbondingHeight)][0])
}

func (s *KeeperTestSuite) TestGetAllUnbondingValidators() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))

	// add ready to unbond validator
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, val))

	// add another unbonding validator
	valAddr1 := sdk.ValAddress(PKs[1].Address().Bytes())
	validator1 := testutil.NewValidator(s.T(), valAddr1, PKs[1])
	valUnbondingHeight1 := blockHeight - 10
	valUnbondingTime1 := blockTime.Add(-1 * time.Minute)
	validator1.UnbondingHeight = valUnbondingHeight1
	validator1.UnbondingTime = valUnbondingTime1
	validator1.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator1))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, validator1))

	// get all unbonding validators should return the inserted validators
	unbondingValidators, err := keeper.GetAllUnbondingValidators(ctx)
	require.NoError(err)
	require.Equal(2, len(unbondingValidators))
	require.Equal(val.GetOperator(), unbondingValidators[stakingtypes.GetCacheValidatorQueueKey(val.UnbondingTime, val.UnbondingHeight)][0])
	require.Equal(validator1.GetOperator(), unbondingValidators[stakingtypes.GetCacheValidatorQueueKey(validator1.UnbondingTime, validator1.UnbondingHeight)][0])
}

func (s *KeeperTestSuite) TestSetUnbondingValidatorQueueCache() {

	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))

	// add ready to unbond validator directly to cache
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.SetUnbondingValidatorQueueCache(ctx, blockTime, blockHeight, []string{val.GetOperator()}))

	// cache should be populated with unbonding validator
	require.Equal(1, len(keeper.GetUnbondingValidatorsCache(ctx)))
	require.Equal(val.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(blockTime, blockHeight)][0])

}

func (s *KeeperTestSuite) TestSetUnbondingValidatorQueueStore() {

	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	iterator, err := keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// no unbonding validator in the queue initially
	require.Equal(0, count)

	// add ready to unbond validator directly to store
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.SetUnbondingValidatorQueueStore(ctx, blockTime, blockHeight, []string{val.GetOperator()}))

	iterator1, err := keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator1.Close()
	count1 := 0
	for ; iterator1.Valid(); iterator1.Next() {
		count1++
	}

	// unbonding validator should be retrieved
	require.Equal(1, count1)

}

func (s *KeeperTestSuite) TestInsertUnbondingValidatorQueue() {

	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	iterator, err := keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// no unbonding validator in the queue initially
	require.Equal(0, count)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))

	// add ready to unbond validator
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, val))

	// add another unbonding validator with same unbonding time and height
	valAddr1 := sdk.ValAddress(PKs[1].Address().Bytes())
	validator1 := testutil.NewValidator(s.T(), valAddr1, PKs[1])
	valUnbondingHeight1 := blockHeight
	valUnbondingTime1 := blockTime
	validator1.UnbondingHeight = valUnbondingHeight1
	validator1.UnbondingTime = valUnbondingTime1
	validator1.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator1))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, validator1))

	iterator1, err := keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator1.Close()
	count1 := 0
	for ; iterator1.Valid(); iterator1.Next() {
		count1++
	}

	// unbonding validator should be retrieved
	// count 1 due to same unbonding time and height
	require.Equal(1, count1)

	// cache should be populated with unbonding validators
	require.Equal(1, len(keeper.GetUnbondingValidatorsCache(ctx))) // length 1 due to same unbonding time and height
	require.Equal(val.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(blockTime, blockHeight)][0])
	require.Equal(validator1.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(validator1.UnbondingTime, validator1.UnbondingHeight)][1])

	// add another unbonding validator with different unbonding time and height
	valAddr2 := sdk.ValAddress(PKs[1].Address().Bytes())
	validator2 := testutil.NewValidator(s.T(), valAddr2, PKs[1])
	valUnbondingHeight2 := blockHeight - 10
	valUnbondingTime2 := blockTime.Add(-1 * time.Minute)
	validator2.UnbondingHeight = valUnbondingHeight2
	validator2.UnbondingTime = valUnbondingTime2
	validator2.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator2))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, validator2))

	iterator2, err := keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator2.Close()
	count2 := 0
	for ; iterator2.Valid(); iterator2.Next() {
		count2++
	}

	// unbonding validator should be retrieved
	require.Equal(2, count2)

	// cache should be populated with unbonding validators
	require.Equal(2, len(keeper.GetUnbondingValidatorsCache(ctx)))
	require.Equal(val.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(blockTime, blockHeight)][0])
	require.Equal(validator1.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(validator1.UnbondingTime, validator1.UnbondingHeight)][1])
	require.Equal(validator2.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(validator2.UnbondingTime, validator2.UnbondingHeight)][0])

}

func (s *KeeperTestSuite) TestDeleteFromValidatorQueueCache() {

	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))

	// add ready to unbond validator directly to cache
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.SetUnbondingValidatorQueueCache(ctx, blockTime, blockHeight, []string{val.GetOperator()}))

	// cache should be populated with unbonding validator
	require.Equal(1, len(keeper.GetUnbondingValidatorsCache(ctx)))
	require.Equal(val.GetOperator(), keeper.GetUnbondingValidatorsCache(ctx)[stakingtypes.GetCacheValidatorQueueKey(blockTime, blockHeight)][0])

	keeper.DeleteFromValidatorQueueCache(ctx, blockTime, blockHeight)

	// cache should also remove the removed unbonding validator
	require.Equal(0, len(keeper.GetUnbondingValidatorsCache(ctx)))

}

func (s *KeeperTestSuite) TestDeleteValidatorQueueStore() {

	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// add ready to unbond validator directly to store
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.SetUnbondingValidatorQueueStore(ctx, blockTime, blockHeight, []string{val.GetOperator()}))

	iterator, err := keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator.Close()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	// unbonding validator in the queue
	require.Equal(1, count)
	require.NoError(keeper.DeleteValidatorQueueStore(ctx, blockTime, blockHeight))

	iterator, err = keeper.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	require.NoError(err)
	defer iterator.Close()
	count = 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}

	// unbonding validator should be removed
	require.Equal(0, count)
}

func (s *KeeperTestSuite) TestGetAndParseCacheValidatorQueueKey() {
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	key := stakingtypes.GetCacheValidatorQueueKey(blockTime, blockHeight)
	time, height, err := stakingtypes.ParseCacheValidatorQueueKey(key)
	require.NoError(err)
	require.Equal(blockTime, time)
	require.Equal(blockHeight, height)

}

func (s *KeeperTestSuite) TestUnbondAllMatureValidators() {
	ctx, keeper := s.ctx, s.stakingKeeper
	require := s.Require()

	blockTime := time.Now().UTC()
	blockHeight := int64(1000)
	ctx = ctx.WithBlockHeight(blockHeight).WithBlockTime(blockTime)

	// cache should be empty initially
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))

	// add unbonding validator - ready to unbond
	valPubKey := PKs[0]
	valAddr := sdk.ValAddress(valPubKey.Address().Bytes())
	val := testutil.NewValidator(s.T(), valAddr, valPubKey)
	val.UnbondingHeight = blockHeight
	val.UnbondingTime = blockTime
	val.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, val))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, val))

	// add another unbonding validator
	valAddr1 := sdk.ValAddress(PKs[1].Address().Bytes())
	validator1 := testutil.NewValidator(s.T(), valAddr1, PKs[1])
	valUnbondingHeight1 := blockHeight - 10
	valUnbondingTime1 := blockTime.Add(-1 * time.Minute)
	validator1.UnbondingHeight = valUnbondingHeight1
	validator1.UnbondingTime = valUnbondingTime1
	validator1.Status = stakingtypes.Unbonding
	require.NoError(keeper.SetValidator(ctx, validator1))
	require.NoError(keeper.InsertUnbondingValidatorQueue(ctx, validator1))

	// cache should be populated with unbonding validators
	require.Equal(2, len(keeper.GetUnbondingValidatorsCache(ctx)))

	err := keeper.UnbondAllMatureValidators(ctx)
	require.NoError(err)

	// all unbonding validators should be removed
	v, err := keeper.GetAllValidators(ctx)
	require.NoError(err)
	require.Equal(0, len(v))

	// cache should be empty after unbonding all mature validators
	require.Empty(keeper.GetUnbondingValidatorsCache(ctx))
}
