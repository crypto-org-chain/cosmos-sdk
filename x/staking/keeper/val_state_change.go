package keeper

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	gogotypes "github.com/cosmos/gogoproto/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// BlockValidatorUpdates calculates the ValidatorUpdates for the current block
// Called in each EndBlock
func (k *Keeper) BlockValidatorUpdates(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔵 BlockValidatorUpdates STARTED", "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔵 BlockValidatorUpdates COMPLETED",
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 100*time.Millisecond {
			logger.Warn("⚠️  SLOW BlockValidatorUpdates detected",
				"duration_ms", duration.Milliseconds())
		}
	}()
	// Calculate validator set changes.
	//
	// NOTE: ApplyAndReturnValidatorSetUpdates has to come before
	// UnbondAllMatureValidatorQueue.
	// This fixes a bug when the unbonding period is instant (is the case in
	// some of the tests). The test expected the validator to be completely
	// unbonded after the Endblocker (go from Bonded -> Unbonding during
	// ApplyAndReturnValidatorSetUpdates and then Unbonding -> Unbonded during
	// UnbondAllMatureValidatorQueue).
	validatorUpdates, err := k.ApplyAndReturnValidatorSetUpdates(ctx)
	if err != nil {
		return nil, err
	}

	// unbond all mature validators from the unbonding queue
	err = k.UnbondAllMatureValidators(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// Remove all mature unbonding delegations from the ubd queue.
	ubdStartTime := time.Now()
	logger.Info("📋 Dequeuing mature unbonding delegations", "timestamp", ubdStartTime.Format(time.RFC3339Nano))

	matureUnbonds, err := k.DequeueAllMatureUBDQueue(ctx, sdkCtx.BlockHeader().Time)
	if err != nil {
		return nil, err
	}

	ubdDequeueTime := time.Since(ubdStartTime)
	logger.Info("📋 Mature unbonding delegations DEQUEUED", "count", len(matureUnbonds), "duration_ms", ubdDequeueTime.Milliseconds())

	ubdProcessStartTime := time.Now()
	ubdProcessedCount := 0
	logger.Info("🔄 Processing unbonding delegations", "count", len(matureUnbonds))

	for _, dvPair := range matureUnbonds {
		ubdProcessedCount++
		addr, err := k.validatorAddressCodec.StringToBytes(dvPair.ValidatorAddress)
		if err != nil {
			return nil, err
		}
		delegatorAddress, err := k.authKeeper.AddressCodec().StringToBytes(dvPair.DelegatorAddress)
		if err != nil {
			return nil, err
		}

		balances, err := k.CompleteUnbonding(ctx, delegatorAddress, addr)
		if err != nil {
			continue
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeCompleteUnbonding,
				sdk.NewAttribute(sdk.AttributeKeyAmount, balances.String()),
				sdk.NewAttribute(types.AttributeKeyValidator, dvPair.ValidatorAddress),
				sdk.NewAttribute(types.AttributeKeyDelegator, dvPair.DelegatorAddress),
			),
		)
	}

	ubdProcessDuration := time.Since(ubdProcessStartTime)
	logger.Info("🔄 Unbonding delegations processing COMPLETED",
		"processed_count", ubdProcessedCount,
		"duration_ms", ubdProcessDuration.Milliseconds(),
		"duration_us", ubdProcessDuration.Microseconds())

	if ubdProcessDuration > 20*time.Millisecond && ubdProcessedCount > 0 {
		logger.Warn("⚠️  SLOW unbonding delegation processing",
			"processed_count", ubdProcessedCount,
			"duration_ms", ubdProcessDuration.Milliseconds(),
			"avg_us_per_delegation", ubdProcessDuration.Microseconds()/int64(ubdProcessedCount))
	}

	// Remove all mature redelegations from the red queue.
	redStartTime := time.Now()
	logger.Info("📋 Dequeuing mature redelegations", "timestamp", redStartTime.Format(time.RFC3339Nano))

	matureRedelegations, err := k.DequeueAllMatureRedelegationQueue(ctx, sdkCtx.BlockHeader().Time)
	if err != nil {
		return nil, err
	}

	redDequeueTime := time.Since(redStartTime)
	logger.Info("📋 Mature redelegations DEQUEUED", "count", len(matureRedelegations), "duration_ms", redDequeueTime.Milliseconds())

	redProcessStartTime := time.Now()
	redProcessedCount := 0
	logger.Info("🔄 Processing redelegations", "count", len(matureRedelegations))

	for _, dvvTriplet := range matureRedelegations {
		redProcessedCount++
		valSrcAddr, err := k.validatorAddressCodec.StringToBytes(dvvTriplet.ValidatorSrcAddress)
		if err != nil {
			return nil, err
		}
		valDstAddr, err := k.validatorAddressCodec.StringToBytes(dvvTriplet.ValidatorDstAddress)
		if err != nil {
			return nil, err
		}
		delegatorAddress, err := k.authKeeper.AddressCodec().StringToBytes(dvvTriplet.DelegatorAddress)
		if err != nil {
			return nil, err
		}

		balances, err := k.CompleteRedelegation(
			ctx,
			delegatorAddress,
			valSrcAddr,
			valDstAddr,
		)
		if err != nil {
			continue
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeCompleteRedelegation,
				sdk.NewAttribute(sdk.AttributeKeyAmount, balances.String()),
				sdk.NewAttribute(types.AttributeKeyDelegator, dvvTriplet.DelegatorAddress),
				sdk.NewAttribute(types.AttributeKeySrcValidator, dvvTriplet.ValidatorSrcAddress),
				sdk.NewAttribute(types.AttributeKeyDstValidator, dvvTriplet.ValidatorDstAddress),
			),
		)
	}

	redProcessDuration := time.Since(redProcessStartTime)
	logger.Info("🔄 Redelegations processing COMPLETED",
		"processed_count", redProcessedCount,
		"duration_ms", redProcessDuration.Milliseconds(),
		"duration_us", redProcessDuration.Microseconds())

	if redProcessDuration > 20*time.Millisecond && redProcessedCount > 0 {
		logger.Warn("⚠️  SLOW redelegation processing",
			"processed_count", redProcessedCount,
			"duration_ms", redProcessDuration.Milliseconds(),
			"avg_us_per_redelegation", redProcessDuration.Microseconds()/int64(redProcessedCount))
	}

	blockTime := sdkCtx.BlockTime()

	k.SetQueueLastProcessedTimestamp(blockTime)

	logger.Info("🔍 QueueLastProcessedState", "timestamp", k.GetQueueLastProcessedState().Timestamp, "height", k.GetQueueLastProcessedState().Height)
	return validatorUpdates, nil
}

// ApplyAndReturnValidatorSetUpdates applies and return accumulated updates to the bonded validator set. Also,
// * Updates the active valset as keyed by LastValidatorPowerKey.
// * Updates the total power as keyed by LastTotalPowerKey.
// * Updates validator status' according to updated powers.
// * Updates the fee pool bonded vs not-bonded tokens.
// * Updates relevant indices.
// It gets called once after genesis, another time maybe after genesis transactions,
// then once at every EndBlock.
//
// CONTRACT: Only validators with non-zero power or zero-power that were bonded
// at the previous block height or were removed from the validator set entirely
// are returned to CometBFT.
func (k Keeper) ApplyAndReturnValidatorSetUpdates(ctx context.Context) (updates []abci.ValidatorUpdate, err error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔵 ApplyAndReturnValidatorSetUpdates STARTED", "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔵 ApplyAndReturnValidatorSetUpdates COMPLETED",
			"updates_count", len(updates),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 200*time.Millisecond {
			logger.Warn("⚠️  SLOW ApplyAndReturnValidatorSetUpdates detected",
				"updates_count", len(updates),
				"duration_ms", duration.Milliseconds())
		}
	}()
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	maxValidators := params.MaxValidators
	powerReduction := k.PowerReduction(ctx)
	totalPower := math.ZeroInt()
	amtFromBondedToNotBonded, amtFromNotBondedToBonded := math.ZeroInt(), math.ZeroInt()

	// Retrieve the last validator set.
	// The persistent set is updated later in this function.
	// (see LastValidatorPowerKey).
	getLastStartTime := time.Now()
	logger.Info("🔍 Retrieving last validator set", "timestamp", getLastStartTime.Format(time.RFC3339Nano))

	last, err := k.getLastValidatorsByAddr(ctx)
	if err != nil {
		return nil, err
	}

	getLastDuration := time.Since(getLastStartTime)
	logger.Info("🔍 Last validator set RETRIEVED", "count", len(last), "duration_ms", getLastDuration.Milliseconds())

	// Iterate over validators, highest power to lowest.
	iteratorStartTime := time.Now()
	logger.Info("🔄 Creating validators power iterator", "timestamp", iteratorStartTime.Format(time.RFC3339Nano))

	iterator, err := k.ValidatorsPowerStoreIterator(ctx)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	iteratorCreateDuration := time.Since(iteratorStartTime)
	logger.Info("🔄 Validators power iterator CREATED", "duration_ms", iteratorCreateDuration.Milliseconds())

	loopStartTime := time.Now()
	validatorCount := 0
	logger.Info("🔄 Starting validator power iteration", "max_validators", maxValidators)

	for count := 0; iterator.Valid() && count < int(maxValidators); iterator.Next() {
		validatorCount++
		// everything that is iterated in this loop is becoming or already a
		// part of the bonded validator set
		valAddr := sdk.ValAddress(iterator.Value())
		validator := k.mustGetValidator(ctx, valAddr)

		if validator.Jailed {
			panic("should never retrieve a jailed validator from the power store")
		}

		// if we get to a zero-power validator (which we don't bond),
		// there are no more possible bonded validators
		if validator.PotentialConsensusPower(k.PowerReduction(ctx)) == 0 {
			break
		}

		// apply the appropriate state change if necessary
		switch {
		case validator.IsUnbonded():
			validator, err = k.unbondedToBonded(ctx, validator)
			if err != nil {
				return
			}
			amtFromNotBondedToBonded = amtFromNotBondedToBonded.Add(validator.GetTokens())
		case validator.IsUnbonding():
			validator, err = k.unbondingToBonded(ctx, validator)
			if err != nil {
				return
			}
			amtFromNotBondedToBonded = amtFromNotBondedToBonded.Add(validator.GetTokens())
		case validator.IsBonded():
			// no state change
		default:
			panic("unexpected validator status")
		}

		valAddrStr := string(valAddr)
		// fetch the old power bytes
		oldPower, found := last[valAddrStr]
		newPower := validator.ConsensusPower(powerReduction)

		// update the validator set if power has changed
		if !found || oldPower != newPower {
			updates = append(updates, validator.ABCIValidatorUpdate(powerReduction))

			if err = k.SetLastValidatorPower(ctx, valAddr, newPower); err != nil {
				return nil, err
			}
		}

		delete(last, valAddrStr)
		count++

		totalPower = totalPower.AddRaw(newPower)
	}

	loopDuration := time.Since(loopStartTime)
	logger.Info("🔄 Validator power iteration COMPLETED",
		"validators_processed", validatorCount,
		"duration_ms", loopDuration.Milliseconds(),
		"duration_us", loopDuration.Microseconds(),
		"avg_us_per_validator", func() int64 {
			if validatorCount > 0 {
				return loopDuration.Microseconds() / int64(validatorCount)
			}
			return 0
		}())

	if loopDuration > 50*time.Millisecond && validatorCount > 0 {
		logger.Warn("⚠️  SLOW validator power iteration",
			"validators", validatorCount,
			"duration_ms", loopDuration.Milliseconds(),
			"avg_us_per_validator", loopDuration.Microseconds()/int64(validatorCount))
	}

	sortStartTime := time.Now()
	logger.Info("🔍 Sorting no longer bonded validators", "count", len(last))

	noLongerBonded, err := sortNoLongerBonded(last, k.validatorAddressCodec)
	if err != nil {
		return nil, err
	}

	sortDuration := time.Since(sortStartTime)
	logger.Info("🔍 No longer bonded validators SORTED", "count", len(noLongerBonded), "duration_ms", sortDuration.Milliseconds())

	unbondingStartTime := time.Now()
	unbondingCount := 0
	logger.Info("🔄 Starting unbonding of no longer bonded validators")

	for _, valAddrBytes := range noLongerBonded {
		unbondingCount++
		validator := k.mustGetValidator(ctx, sdk.ValAddress(valAddrBytes))
		validator, err = k.bondedToUnbonding(ctx, validator)
		if err != nil {
			return nil, err
		}
		str, err := k.validatorAddressCodec.StringToBytes(validator.GetOperator())
		if err != nil {
			return nil, err
		}
		amtFromBondedToNotBonded = amtFromBondedToNotBonded.Add(validator.GetTokens())
		if err = k.DeleteLastValidatorPower(ctx, str); err != nil {
			return nil, err
		}

		updates = append(updates, validator.ABCIValidatorUpdateZero())
	}

	unbondingDuration := time.Since(unbondingStartTime)
	logger.Info("🔄 Unbonding of no longer bonded validators COMPLETED",
		"validators_unbonded", unbondingCount,
		"duration_ms", unbondingDuration.Milliseconds())

	if unbondingDuration > 20*time.Millisecond && unbondingCount > 0 {
		logger.Warn("⚠️  SLOW validator unbonding",
			"validators", unbondingCount,
			"duration_ms", unbondingDuration.Milliseconds())
	}

	// Update the pools based on the recent updates in the validator set:
	// - The tokens from the non-bonded candidates that enter the new validator set need to be transferred
	// to the Bonded pool.
	// - The tokens from the bonded validators that are being kicked out from the validator set
	// need to be transferred to the NotBonded pool.
	switch {
	// Compare and subtract the respective amounts to only perform one transfer.
	// This is done in order to avoid doing multiple updates inside each iterator/loop.
	case amtFromNotBondedToBonded.GT(amtFromBondedToNotBonded):
		if err = k.notBondedTokensToBonded(ctx, amtFromNotBondedToBonded.Sub(amtFromBondedToNotBonded)); err != nil {
			return nil, err
		}
	case amtFromNotBondedToBonded.LT(amtFromBondedToNotBonded):
		if err = k.bondedTokensToNotBonded(ctx, amtFromBondedToNotBonded.Sub(amtFromNotBondedToBonded)); err != nil {
			return nil, err
		}
	default: // equal amounts of tokens; no update required
	}

	// set total power on lookup index if there are any updates
	if len(updates) > 0 {
		if err = k.SetLastTotalPower(ctx, totalPower); err != nil {
			return nil, err
		}
	}

	// set the list of validator updates
	if err = k.SetValidatorUpdates(ctx, updates); err != nil {
		return nil, err
	}

	return updates, err
}

// Validator state transitions

func (k Keeper) bondedToUnbonding(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔄 bondedToUnbonding STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔄 bondedToUnbonding COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 10*time.Millisecond {
			logger.Warn("⚠️  SLOW bondedToUnbonding detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	if !validator.IsBonded() {
		panic(fmt.Sprintf("bad state transition bondedToUnbonding, validator: %v\n", validator))
	}

	return k.BeginUnbondingValidator(ctx, validator)
}

func (k Keeper) unbondingToBonded(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔄 unbondingToBonded STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔄 unbondingToBonded COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 10*time.Millisecond {
			logger.Warn("⚠️  SLOW unbondingToBonded detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	if !validator.IsUnbonding() {
		panic(fmt.Sprintf("bad state transition unbondingToBonded, validator: %v\n", validator))
	}

	return k.bondValidator(ctx, validator)
}

func (k Keeper) unbondedToBonded(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔄 unbondedToBonded STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔄 unbondedToBonded COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 10*time.Millisecond {
			logger.Warn("⚠️  SLOW unbondedToBonded detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	if !validator.IsUnbonded() {
		panic(fmt.Sprintf("bad state transition unbondedToBonded, validator: %v\n", validator))
	}

	return k.bondValidator(ctx, validator)
}

// UnbondingToUnbonded switches a validator from unbonding state to unbonded state
func (k Keeper) UnbondingToUnbonded(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔄 UnbondingToUnbonded STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔄 UnbondingToUnbonded COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 10*time.Millisecond {
			logger.Warn("⚠️  SLOW UnbondingToUnbonded detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	if !validator.IsUnbonding() {
		return types.Validator{}, fmt.Errorf("bad state transition unbondingToUnbonded, validator: %v", validator)
	}

	return k.completeUnbondingValidator(ctx, validator)
}

// send a validator to jail
func (k Keeper) jailValidator(ctx context.Context, validator types.Validator) error {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔒 jailValidator STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔒 jailValidator COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 20*time.Millisecond {
			logger.Warn("⚠️  SLOW jailValidator detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	if validator.Jailed {
		return types.ErrValidatorJailed.Wrapf("cannot jail already jailed validator, validator: %v", validator)
	}

	validator.Jailed = true
	if err := k.SetValidator(ctx, validator); err != nil {
		return err
	}

	return k.DeleteValidatorByPowerIndex(ctx, validator)
}

// remove a validator from jail
func (k Keeper) unjailValidator(ctx context.Context, validator types.Validator) error {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔓 unjailValidator STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔓 unjailValidator COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 20*time.Millisecond {
			logger.Warn("⚠️  SLOW unjailValidator detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	if !validator.Jailed {
		return fmt.Errorf("cannot unjail already unjailed validator, validator: %v", validator)
	}

	validator.Jailed = false
	if err := k.SetValidator(ctx, validator); err != nil {
		return err
	}

	return k.SetValidatorByPowerIndex(ctx, validator)
}

// perform all the store operations for when a validator status becomes bonded
func (k Keeper) bondValidator(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔗 bondValidator STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔗 bondValidator COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 30*time.Millisecond {
			logger.Warn("⚠️  SLOW bondValidator detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	// delete the validator by power index, as the key will change
	if err := k.DeleteValidatorByPowerIndex(ctx, validator); err != nil {
		return validator, err
	}

	validator = validator.UpdateStatus(types.Bonded)

	// save the now bonded validator record to the two referenced stores
	if err := k.SetValidator(ctx, validator); err != nil {
		return validator, err
	}

	if err := k.SetValidatorByPowerIndex(ctx, validator); err != nil {
		return validator, err
	}

	// delete from queue if present
	if err := k.DeleteValidatorQueue(ctx, validator); err != nil {
		return validator, err
	}

	// trigger hook
	consAddr, err := validator.GetConsAddr()
	if err != nil {
		return validator, err
	}

	str, err := k.validatorAddressCodec.StringToBytes(validator.GetOperator())
	if err != nil {
		return validator, err
	}

	if err := k.Hooks().AfterValidatorBonded(ctx, consAddr, str); err != nil {
		return validator, err
	}

	return validator, err
}

// BeginUnbondingValidator performs all the store operations for when a validator begins unbonding
func (k Keeper) BeginUnbondingValidator(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔗 BeginUnbondingValidator STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔗 BeginUnbondingValidator COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 50*time.Millisecond {
			logger.Warn("⚠️  SLOW BeginUnbondingValidator detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	paramsStartTime := time.Now()
	logger.Info("🔍 Getting unbonding parameters")

	params, err := k.GetParams(ctx)
	if err != nil {
		return validator, err
	}

	paramsDuration := time.Since(paramsStartTime)
	logger.Info("🔍 Parameters retrieved", "unbonding_time", params.UnbondingTime, "duration_ms", paramsDuration.Milliseconds())

	// delete the validator by power index, as the key will change
	deleteStartTime := time.Now()
	logger.Info("🗑️ Deleting validator by power index")

	if err = k.DeleteValidatorByPowerIndex(ctx, validator); err != nil {
		return validator, err
	}

	deleteDuration := time.Since(deleteStartTime)
	logger.Info("🗑️ Validator deleted by power index", "duration_ms", deleteDuration.Milliseconds())

	// sanity check
	if validator.Status != types.Bonded {
		panic(fmt.Sprintf("should not already be unbonded or unbonding, validator: %v\n", validator))
	}

	id, err := k.IncrementUnbondingID(ctx)
	if err != nil {
		return validator, err
	}

	validator = validator.UpdateStatus(types.Unbonding)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// set the unbonding completion time and completion height appropriately
	validator.UnbondingTime = sdkCtx.BlockHeader().Time.Add(params.UnbondingTime)
	validator.UnbondingHeight = sdkCtx.BlockHeader().Height

	validator.UnbondingIds = append(validator.UnbondingIds, id)

	// save the now unbonded validator record and power index
	if err = k.SetValidator(ctx, validator); err != nil {
		return validator, err
	}

	if err = k.SetValidatorByPowerIndex(ctx, validator); err != nil {
		return validator, err
	}

	// Adds to unbonding validator queue
	queueStartTime := time.Now()
	logger.Info("📋 Adding to unbonding validator queue")

	if err = k.InsertUnbondingValidatorQueue(ctx, validator); err != nil {
		return validator, err
	}

	queueDuration := time.Since(queueStartTime)
	logger.Info("📋 Added to unbonding validator queue", "duration_ms", queueDuration.Milliseconds())

	// trigger hook
	hookStartTime := time.Now()
	logger.Info("🪝 Triggering unbonding hooks")

	consAddr, err := validator.GetConsAddr()
	if err != nil {
		return validator, err
	}

	str, err := k.validatorAddressCodec.StringToBytes(validator.GetOperator())
	if err != nil {
		return validator, err
	}

	if err := k.Hooks().AfterValidatorBeginUnbonding(ctx, consAddr, str); err != nil {
		return validator, err
	}

	if err := k.SetValidatorByUnbondingID(ctx, validator, id); err != nil {
		return validator, err
	}

	if err := k.Hooks().AfterUnbondingInitiated(ctx, id); err != nil {
		return validator, err
	}

	hookDuration := time.Since(hookStartTime)
	logger.Info("🪝 Unbonding hooks COMPLETED", "duration_ms", hookDuration.Milliseconds())

	if hookDuration > 10*time.Millisecond {
		logger.Warn("⚠️  SLOW unbonding hooks", "duration_ms", hookDuration.Milliseconds())
	}

	return validator, nil
}

// perform all the store operations for when a validator status becomes unbonded
func (k Keeper) completeUnbondingValidator(ctx context.Context, validator types.Validator) (types.Validator, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)

	logger.Info("🔗 completeUnbondingValidator STARTED", "validator", validator.GetOperator(), "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🔗 completeUnbondingValidator COMPLETED",
			"validator", validator.GetOperator(),
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds())

		if duration > 20*time.Millisecond {
			logger.Warn("⚠️  SLOW completeUnbondingValidator detected",
				"validator", validator.GetOperator(),
				"duration_ms", duration.Milliseconds())
		}
	}()
	validator = validator.UpdateStatus(types.Unbonded)
	if err := k.SetValidator(ctx, validator); err != nil {
		return validator, err
	}

	return validator, nil
}

// map of operator addresses to power
// We use (non bech32) strings here, because we can't have slices as keys: map[[]byte][]byte
type validatorsByAddr map[string]int64

// get the last validator set
func (k Keeper) getLastValidatorsByAddr(ctx context.Context) (validatorsByAddr, error) {
	last := make(validatorsByAddr)

	iterator, err := k.LastValidatorsIterator(ctx)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	var intVal gogotypes.Int64Value
	for ; iterator.Valid(); iterator.Next() {
		// extract the validator address from the key (prefix is 1-byte, addrLen is 1-byte)
		valAddrStr := string(types.AddressFromLastValidatorPowerKey(iterator.Key()))
		k.cdc.MustUnmarshal(iterator.Value(), &intVal)
		last[valAddrStr] = intVal.GetValue()
	}

	return last, nil
}

// given a map of remaining validators to previous bonded power
// returns the list of validators to be unbonded, sorted by operator address
func sortNoLongerBonded(last validatorsByAddr, ac address.Codec) ([][]byte, error) {
	// sort the map keys for determinism
	noLongerBonded := make([][]byte, len(last))
	index := 0

	for valAddrStr := range last {
		valAddrBytes := []byte(valAddrStr)
		noLongerBonded[index] = valAddrBytes
		index++
	}
	// sorted by address - order doesn't matter
	sort.SliceStable(noLongerBonded, func(i, j int) bool {
		// -1 means strictly less than
		return bytes.Compare(noLongerBonded[i], noLongerBonded[j]) == -1
	})

	return noLongerBonded, nil
}
