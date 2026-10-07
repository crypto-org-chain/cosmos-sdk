package tx_test

import (
	"context"
	"testing"

	"github.com/cometbft/cometbft/rpc/client/mock"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/client"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
)

type ctxKey struct{}

// ctxRecordingClient returns one tx and records the context each CometBFT
// call receives, keyed by method name.
type ctxRecordingClient struct {
	mock.Client
	txBytes []byte
	ctxs    map[string]context.Context
}

func (c *ctxRecordingClient) TxSearch(ctx context.Context, _ string, _ bool, _, _ *int, _ string) (*coretypes.ResultTxSearch, error) {
	c.ctxs["TxSearch"] = ctx
	return &coretypes.ResultTxSearch{Txs: []*coretypes.ResultTx{{Tx: c.txBytes, Height: 1}}, TotalCount: 1}, nil
}

func (c *ctxRecordingClient) Tx(ctx context.Context, _ []byte, _ bool) (*coretypes.ResultTx, error) {
	c.ctxs["Tx"] = ctx
	return &coretypes.ResultTx{Tx: c.txBytes, Height: 1}, nil
}

func (c *ctxRecordingClient) Block(ctx context.Context, _ *int64) (*coretypes.ResultBlock, error) {
	c.ctxs["Block"] = ctx
	return &coretypes.ResultBlock{Block: &cmttypes.Block{}}, nil
}

type TxServiceTestSuite struct {
	suite.Suite

	node   *ctxRecordingClient
	server txtypes.ServiceServer
}

func TestTxServiceTestSuite(t *testing.T) {
	suite.Run(t, new(TxServiceTestSuite))
}

func (s *TxServiceTestSuite) SetupTest() {
	encCfg := moduletestutil.MakeTestEncodingConfig()
	txBytes, err := encCfg.TxConfig.TxEncoder()(encCfg.TxConfig.NewTxBuilder().GetTx())
	s.Require().NoError(err)

	s.node = &ctxRecordingClient{txBytes: txBytes, ctxs: map[string]context.Context{}}
	clientCtx := client.Context{}.WithClient(s.node).WithTxConfig(encCfg.TxConfig)
	s.server = authtx.NewTxServer(clientCtx, nil, encCfg.InterfaceRegistry)
}

func (s *TxServiceTestSuite) TestRequestContextReachesNode() {
	testCases := []struct {
		name     string
		call     func(ctx context.Context) error
		expCalls []string
	}{
		{
			name: "GetTxsEvent",
			call: func(ctx context.Context) error {
				_, err := s.server.GetTxsEvent(ctx, &txtypes.GetTxsEventRequest{Query: "message.sender='cosmos1'", Page: 1, Limit: 1})
				return err
			},
			expCalls: []string{"TxSearch", "Block"},
		},
		{
			name: "GetTx",
			call: func(ctx context.Context) error {
				_, err := s.server.GetTx(ctx, &txtypes.GetTxRequest{Hash: "AB"})
				return err
			},
			expCalls: []string{"Tx", "Block"},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			ctx := context.WithValue(context.Background(), ctxKey{}, tc.name)

			s.Require().NoError(tc.call(ctx))
			s.Require().Len(s.node.ctxs, len(tc.expCalls))
			for _, method := range tc.expCalls {
				s.Require().Contains(s.node.ctxs, method)
				s.Require().Equal(tc.name, s.node.ctxs[method].Value(ctxKey{}), method)
			}
		})
	}
}
