package types_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/testutil"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

func TestMultiStakingHooksRedelegation(t *testing.T) {
	ctx := context.Background()
	delAddr := sdk.AccAddress("delegator")
	valSrcAddr, valDstAddr := sdk.ValAddress("source"), sdk.ValAddress("destination")
	unbondingIDs := []uint64{7, 8}
	shares := math.LegacyNewDec(5)
	hookErr := errors.New("hook failed")

	beforeSlashed := func(h *testutil.MockStakingHooks) *gomock.Call {
		return h.EXPECT().BeforeRedelegationSlashed(ctx, unbondingIDs[0], shares)
	}
	callBeforeSlashed := func(h types.StakingHooks) error {
		return h.BeforeRedelegationSlashed(ctx, unbondingIDs[0], shares)
	}
	afterCompleted := func(h *testutil.MockStakingHooks) *gomock.Call {
		return h.EXPECT().AfterRedelegationCompleted(ctx, delAddr, valSrcAddr, valDstAddr, unbondingIDs)
	}
	callAfterCompleted := func(h types.StakingHooks) error {
		return h.AfterRedelegationCompleted(ctx, delAddr, valSrcAddr, valDstAddr, unbondingIDs)
	}

	testCases := []struct {
		name     string
		expect   func(*testutil.MockStakingHooks) *gomock.Call
		call     func(types.StakingHooks) error
		firstErr error
	}{
		{name: "BeforeRedelegationSlashed reaches every hook", expect: beforeSlashed, call: callBeforeSlashed},
		{name: "BeforeRedelegationSlashed stops at the first error", expect: beforeSlashed, call: callBeforeSlashed, firstErr: hookErr},
		{name: "AfterRedelegationCompleted reaches every hook", expect: afterCompleted, call: callAfterCompleted},
		{name: "AfterRedelegationCompleted stops at the first error", expect: afterCompleted, call: callAfterCompleted, firstErr: hookErr},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			first, second := testutil.NewMockStakingHooks(ctrl), testutil.NewMockStakingHooks(ctrl)
			tc.expect(first).Return(tc.firstErr)
			if tc.firstErr == nil {
				tc.expect(second).Return(nil)
			}

			err := tc.call(types.NewMultiStakingHooks(first, second))
			if tc.firstErr != nil {
				require.ErrorIs(t, err, tc.firstErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
