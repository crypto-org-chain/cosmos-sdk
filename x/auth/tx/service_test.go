package tx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cometbft/cometbft/rpc/client/mock"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
)

type ctxKey struct{}

// ctxRecordingClient records the context each CometBFT call receives.
type ctxRecordingClient struct {
	mock.Client
	ctx context.Context
}

func (c *ctxRecordingClient) TxSearch(ctx context.Context, _ string, _ bool, _, _ *int, _ string) (*coretypes.ResultTxSearch, error) {
	c.ctx = ctx
	return &coretypes.ResultTxSearch{}, nil
}

func (c *ctxRecordingClient) Tx(ctx context.Context, _ []byte, _ bool) (*coretypes.ResultTx, error) {
	c.ctx = ctx
	return nil, errors.New("tx not found")
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
	s.node = &ctxRecordingClient{}
	clientCtx := client.Context{}.WithClient(s.node)
	s.server = authtx.NewTxServer(clientCtx, nil, types.NewInterfaceRegistry())
}

func (s *TxServiceTestSuite) TestRequestContextReachesNode() {
	testCases := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{
			name: "GetTxsEvent",
			call: func(ctx context.Context) error {
				_, err := s.server.GetTxsEvent(ctx, &txtypes.GetTxsEventRequest{Query: "message.sender='cosmos1'", Page: 1, Limit: 1})
				return err
			},
		},
		{
			name: "GetTx",
			call: func(ctx context.Context) error {
				_, err := s.server.GetTx(ctx, &txtypes.GetTxRequest{Hash: "AB"})
				return err
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			ctx := context.WithValue(context.Background(), ctxKey{}, tc.name)

			_ = tc.call(ctx)

			s.Require().NotNil(s.node.ctx)
			s.Require().Equal(tc.name, s.node.ctx.Value(ctxKey{}))
		})
	}
}
