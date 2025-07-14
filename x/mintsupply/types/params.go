package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// NewParams creates a new Params object
func NewParams(maxSupply math.Int) Params {
	return Params{
		MaxSupply: maxSupply,
	}
}

// DefaultParams returns a default set of parameters, sets the max supply to zero means there is no limit.
func DefaultParams() Params {
	return NewParams(
		math.NewInt(0),
	)
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := validateMaxSupply(p.MaxSupply); err != nil {
		return err
	}
	return nil
}

func validateMaxSupply(maxSupply math.Int) error {
	if maxSupply.IsNegative() {
		return fmt.Errorf("max supply cannot be negative: %s", maxSupply)
	}

	return nil
}
