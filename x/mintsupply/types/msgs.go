package types

import (
	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
)

// GetSigners returns the expected signers for a MsgUpdateParams message.
func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.Authority)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if err := m.Params.Validate(); err != nil {
		return err
	}

	return nil
}

// Route implements the LegacyMsg interface.
func (m *MsgUpdateParams) Route() string { return RouterKey }

// Type implements the LegacyMsg interface.
func (m *MsgUpdateParams) Type() string { return TypeMsgUpdateParams }
