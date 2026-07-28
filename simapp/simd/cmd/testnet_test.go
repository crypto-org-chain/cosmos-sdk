package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	cmtconfig "github.com/cometbft/cometbft/config"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltest "github.com/cosmos/cosmos-sdk/x/genutil/client/testutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/mint"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

func Test_TestnetCmd(t *testing.T) {
	moduleBasic := module.NewBasicManager(
		auth.AppModuleBasic{},
		genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
		bank.AppModuleBasic{},
		staking.AppModuleBasic{},
		mint.AppModuleBasic{},
		distribution.AppModuleBasic{},
		consensus.AppModuleBasic{},
	)

	home := t.TempDir()
	encodingConfig := moduletestutil.MakeTestEncodingConfig(auth.AppModuleBasic{}, staking.AppModuleBasic{})
	logger := log.NewNopLogger()
	cfg, err := genutiltest.CreateDefaultCometConfig(home)
	require.NoError(t, err)

	err = genutiltest.ExecInitCmd(moduleBasic, home, encodingConfig.Codec)
	require.NoError(t, err)

	serverCtx := server.NewContext(viper.New(), cfg, logger)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithHomeDir(home).
		WithTxConfig(encodingConfig.TxConfig)

	ctx := context.Background()
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	cmd := testnetInitFilesCmd(moduleBasic, banktypes.GenesisBalancesIterator{})
	cmd.SetArgs([]string{fmt.Sprintf("--%s=test", flags.FlagKeyringBackend), fmt.Sprintf("--output-dir=%s", home)})
	err = cmd.ExecuteContext(ctx)
	require.NoError(t, err)

	genFile := cfg.GenesisFile()
	appState, _, err := genutiltypes.GenesisStateFromGenFile(genFile)
	require.NoError(t, err)

	bankGenState := banktypes.GetGenesisStateFromAppState(encodingConfig.Codec, appState)
	require.NotEmpty(t, bankGenState.Supply.String())
}

// Test_TestnetCmd_SingleHostUniquePorts guards against a regression where collectGenFiles
// wrote every node's config.toml using the last node's pprof/prometheus ports because
// nodeConfig was a single pointer mutated across two sequential loops.
func Test_TestnetCmd_SingleHostUniquePorts(t *testing.T) {
	moduleBasic := module.NewBasicManager(
		auth.AppModuleBasic{},
		genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
		bank.AppModuleBasic{},
		staking.AppModuleBasic{},
		mint.AppModuleBasic{},
		distribution.AppModuleBasic{},
		consensus.AppModuleBasic{},
	)

	home := t.TempDir()
	encodingConfig := moduletestutil.MakeTestEncodingConfig(auth.AppModuleBasic{}, staking.AppModuleBasic{})
	logger := log.NewNopLogger()
	cfg, err := genutiltest.CreateDefaultCometConfig(home)
	require.NoError(t, err)

	err = genutiltest.ExecInitCmd(moduleBasic, home, encodingConfig.Codec)
	require.NoError(t, err)

	serverCtx := server.NewContext(viper.New(), cfg, logger)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithHomeDir(home).
		WithTxConfig(encodingConfig.TxConfig)

	ctx := context.Background()
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	cmd := testnetInitFilesCmd(moduleBasic, banktypes.GenesisBalancesIterator{})
	cmd.SetArgs([]string{
		fmt.Sprintf("--%s=test", flags.FlagKeyringBackend),
		fmt.Sprintf("--output-dir=%s", home),
		fmt.Sprintf("--%s=4", flagNumValidators),
		fmt.Sprintf("--%s", flagSingleHost),
	})
	err = cmd.ExecuteContext(ctx)
	require.NoError(t, err)

	seenPprof := make(map[string]int)
	seenPrometheus := make(map[string]int)
	for i := range 4 {
		nodeDir := filepath.Join(home, fmt.Sprintf("node%d", i), "simd", "config", "config.toml")
		nodeCfg := cmtconfig.DefaultConfig()
		viper.SetConfigFile(nodeDir)
		require.NoError(t, viper.ReadInConfig())
		require.NoError(t, viper.Unmarshal(nodeCfg))

		require.Equal(t, fmt.Sprintf("localhost:%d", 6060+i), nodeCfg.RPC.PprofListenAddress)
		require.Equal(t, fmt.Sprintf(":%d", 27780+i), nodeCfg.Instrumentation.PrometheusListenAddr)

		seenPprof[nodeCfg.RPC.PprofListenAddress]++
		seenPrometheus[nodeCfg.Instrumentation.PrometheusListenAddr]++
	}

	for addr, count := range seenPprof {
		require.Equal(t, 1, count, "pprof address %s reused across nodes", addr)
	}
	for addr, count := range seenPrometheus {
		require.Equal(t, 1, count, "prometheus address %s reused across nodes", addr)
	}
}
