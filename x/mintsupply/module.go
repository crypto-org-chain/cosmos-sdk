package mintsupply

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz/simulation"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/mintsupply/keeper"
	"github.com/cosmos/cosmos-sdk/x/mintsupply/types"

	modulev1 "cosmossdk.io/api/cosmos/mintsupply/module/v1"
)

var (
	_ module.AppModuleBasic      = AppModule{}
	_ module.AppModuleSimulation = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasInvariants       = AppModule{}
	_ module.HasConsensusVersion = AppModule{}

	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
)

// ConsensusVersion defines the current x/mintsupply module consensus version.
const ConsensusVersion = 1

type AppModule struct {
	AppModuleBasic

	cdc    codec.Codec
	keeper keeper.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(
	cdc codec.Codec,
	keeper keeper.Keeper,
) AppModule {
	return AppModule{
		cdc:    cdc,
		keeper: keeper,
	}
}

// AppModuleBasic defines the basic application module used by the mintsupply module.
type AppModuleBasic struct {
	cdc codec.Codec
}

// Name returns the mintsupply module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the mintsupply module's types on the given LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {}

// RegisterInterfaces registers the mintsupply module's interface types
func (b AppModuleBasic) RegisterInterfaces(r codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(r)
}

// DefaultGenesis returns default genesis state as raw bytes for the mintsupply
// module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the mintsupply module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var genState types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &genState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genState.Validate()
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the mint module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func NewAppModuleBasic(m AppModule) module.AppModuleBasic {
	return module.CoreAppModuleBasicAdaptor(m.Name(), m)
}

// // Name returns the mintsupply module's name.
// func (AppModule) Name() string {
// 	return types.ModuleName
// }

// // RegisterLegacyAminoCodec registers the mintsupply module's types on the LegacyAmino codec.
// func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {}

// // RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
// func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
// 	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
// 		panic(err)
// 	}
// }

// RegisterInterfaces registers the module's interface types
func (a AppModule) RegisterInterfaces(reg codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

// ConsensusVersion implements ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }

// RegisterServices registers a GRPC query service to respond to the
// module-specific GRPC queries.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// RegisterInvariants registers the mintsupply module's invariants.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// InitGenesis performs the mintsupply module's genesis initialization It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, gs json.RawMessage) {
	var genState types.GenesisState
	if err := cdc.UnmarshalJSON(gs, &genState); err != nil {
		panic(fmt.Sprintf("failed to unmarshal %s genesis state: %s", types.ModuleName, err))
	}

	if err := am.keeper.InitGenesis(ctx, &genState); err != nil {
		panic(fmt.Sprintf("failed to initialize %s genesis state: %s", types.ModuleName, err))
	}
}

// ExportGenesis returns the mintsupply module's exported genesis state as raw JSON bytes.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genState, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to export %s genesis state: %s", types.ModuleName, err))
	}

	return cdc.MustMarshalJSON(genState)
}

// DefaultGenesis returns the mintsupply module's default genesis state.
// func (AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
// 	return cdc.MustMarshalJSON(types.DefaultGenesis())
// }

// ValidateGenesis performs genesis state validation for the mintsupply module.
// func (AppModule) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
// 	var genState types.GenesisState
// 	if err := cdc.UnmarshalJSON(bz, &genState); err != nil {
// 		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
// 	}
// 	return genState.Validate()
// }

// BeginBlock executes all ABCI BeginBlock logic respective to the mintsupply module.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return nil
}

// EndBlock executes all ABCI EndBlock logic respective to the mintsupply module.
func (am AppModule) EndBlock(ctx context.Context) error {
	return nil
}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// GenerateGenesisState creates a randomized GenState of the mintsupply module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers a decoder for mintsupply module's types
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simulation.NewDecodeStore(am.cdc)
}

// WeightedOperations doesn't return any mintsupply module operation.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return nil
}

//
// App Wiring Setup
//

func init() {
	appmodule.Register(
		&modulev1.Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	Cdc          codec.Codec
	StoreService store.KVStoreService
	Logger       log.Logger

	Config *modulev1.Module
}

type ModuleOutputs struct {
	depinject.Out

	MintSupplyKeeper keeper.Keeper
	Module           appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// default to governance authority if not provided
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}

	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.Logger,
		authority.String(),
	)

	m := NewAppModule(
		in.Cdc,
		k,
	)

	return ModuleOutputs{
		MintSupplyKeeper: k,
		Module:           m,
	}
}
