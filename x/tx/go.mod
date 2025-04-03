module cosmossdk.io/x/tx

go 1.21
toolchain go1.24.1

require (
	cosmossdk.io/api v0.9.0
	cosmossdk.io/core v0.11.0
	cosmossdk.io/errors v1.0.1
	cosmossdk.io/math v1.3.0
	github.com/cosmos/cosmos-proto v1.0.0-beta.5
	github.com/cosmos/gogoproto v1.7.0
	github.com/google/go-cmp v0.7.0
	github.com/google/gofuzz v1.2.0
	github.com/iancoleman/strcase v0.3.0
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.9.0
	github.com/tendermint/go-amino v0.16.0
	google.golang.org/protobuf v1.36.6
	gotest.tools/v3 v3.5.1
	pgregory.net/rapid v1.1.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/exp v0.0.0-20240222234643-814bf88cf225 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250324211829-b45e905df463 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250324211829-b45e905df463 // indirect
	google.golang.org/grpc v1.71.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// NOTE: we do not want to replace to the development version of cosmossdk.io/api yet
// Until https://github.com/cosmos/cosmos-sdk/issues/19228 is resolved
// We are tagging x/tx from main and must keep using released versions of x/tx dependencies
