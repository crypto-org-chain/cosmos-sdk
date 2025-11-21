package cache

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

func marshal[V any](cdc codec.BinaryCodec, cacheType Type, value V) ([]byte, error) {
	switch cacheType {
	case UnbondingValidators:
		addrs := any(value).([]string)
		return cdc.Marshal(&types.ValAddresses{Addresses: addrs})
	case UnbondingDelegations:
		pairs := any(value).([]types.DVPair)
		return cdc.Marshal(&types.DVPairs{Pairs: pairs})
	case Redelegations:
		triplets := any(value).([]types.DVVTriplet)
		return cdc.Marshal(&types.DVVTriplets{Triplets: triplets})
	default:
		return nil, fmt.Errorf("unknown cache type: %s", cacheType)
	}
}

func unmarshal[V any](cdc codec.BinaryCodec, cacheType Type, bz []byte) (V, error) {
	var zero V
	switch cacheType {
	case UnbondingValidators:
		var valAddrs types.ValAddresses
		if err := cdc.Unmarshal(bz, &valAddrs); err != nil {
			return zero, err
		}
		return any(valAddrs.Addresses).(V), nil
	case UnbondingDelegations:
		var pairs types.DVPairs
		if err := cdc.Unmarshal(bz, &pairs); err != nil {
			return zero, err
		}
		return any(pairs.Pairs).(V), nil
	case Redelegations:
		var triplets types.DVVTriplets
		if err := cdc.Unmarshal(bz, &triplets); err != nil {
			return zero, err
		}
		return any(triplets.Triplets).(V), nil
	default:
		return zero, fmt.Errorf("unknown cache type: %s", cacheType)
	}
}
