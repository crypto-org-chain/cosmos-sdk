package keeper

import (
	"context"
	"time"
	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// BeginBlocker will persist the current header and validator set as a historical entry
// and prune the oldest entry based on the HistoricalEntries parameter
func (k *Keeper) BeginBlocker(ctx context.Context) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, telemetry.Now(), telemetry.MetricKeyBeginBlocker)
	return k.TrackHistoricalInfo(ctx)
}

// EndBlocker called at every block, update validator set
func (k *Keeper) EndBlocker(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	startTime := time.Now()
	logger := k.Logger(ctx)
	defer telemetry.ModuleMeasureSince(types.ModuleName, telemetry.Now(), telemetry.MetricKeyEndBlocker)

	logger.Info("🟡 STAKING EndBlocker STARTED", "timestamp", startTime.Format(time.RFC3339Nano))

	defer func() {
		duration := time.Since(startTime)
		logger.Info("🟢 STAKING EndBlocker COMPLETED",
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds(),
			"duration_ns", duration.Nanoseconds(),
			"timestamp", time.Now().Format(time.RFC3339Nano))

		// Alert if taking too long
		if duration > 100*time.Millisecond {
			logger.Warn("⚠️  SLOW EndBlocker detected",
				"duration_ms", duration.Milliseconds(),
				"threshold_ms", 100)
		}

		if duration > 1*time.Second {
			logger.Error("🔴 VERY SLOW EndBlocker detected",
				"duration_ms", duration.Milliseconds(),
				"threshold_ms", 1000)
		}
	}()

	return k.BlockValidatorUpdates(ctx)
}
