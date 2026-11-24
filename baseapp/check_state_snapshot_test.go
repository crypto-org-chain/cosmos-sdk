package baseapp_test

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/baseapp"
	baseapptestutil "github.com/cosmos/cosmos-sdk/baseapp/testutil"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const snapshotTestKeys = 8

var (
	snapshotTransientKey = storetypes.NewTransientStoreKey("snapshot-transient")
	snapshotObjectKey    = storetypes.NewObjectStoreKey("snapshot-object")
)

type CheckStateSnapshotSuite struct {
	suite.Suite

	app     *baseapp.BaseApp
	txBytes []byte

	// commitStarted is the highest block whose Commit has begun; a check-state
	// read of anything newer saw uncommitted state.
	commitStarted atomic.Int64
	reads         atomic.Int64

	mu         sync.Mutex
	violations []string
}

func TestCheckStateSnapshotSuite(t *testing.T) {
	suite.Run(t, new(CheckStateSnapshotSuite))
}

func (s *CheckStateSnapshotSuite) SetupTest() {
	s.commitStarted.Store(0)
	s.reads.Store(0)
	s.violations = nil

	bas := NewBaseAppSuite(s.T(), func(app *baseapp.BaseApp) {
		app.MountStores(snapshotTransientKey, snapshotObjectKey)
		app.SetBeginBlocker(func(ctx sdk.Context) (sdk.BeginBlock, error) {
			writeBlockHeight(ctx)
			return sdk.BeginBlock{}, nil
		})
		app.SetAnteHandler(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			s.observe(ctx)
			return ctx, nil
		})
	})
	baseapptestutil.RegisterCounterServer(bas.baseApp.MsgServiceRouter(), NoopCounterServerImpl{})
	s.app = bas.baseApp

	var err error
	s.txBytes, err = bas.txConfig.TxEncoder()(newTxCounter(s.T(), bas.txConfig, 0, 0))
	s.Require().NoError(err)

	_, err = s.app.InitChain(&abci.RequestInitChain{ConsensusParams: &cmtproto.ConsensusParams{}})
	s.Require().NoError(err)
	s.commitBlock(1)
}

func writeBlockHeight(ctx sdk.Context) {
	height := ctx.BlockHeight()
	bz := binary.BigEndian.AppendUint64(nil, uint64(height))
	for i := range snapshotTestKeys {
		key := fmt.Appendf(nil, "height-%d", i)
		ctx.KVStore(capKey1).Set(key, bz)
		ctx.TransientStore(snapshotTransientKey).Set(key, bz)
		ctx.ObjectStore(snapshotObjectKey).Set(key, height)
	}
}

// observe reads every store through a check-state context and records what a
// lock-free reader must never see.
func (s *CheckStateSnapshotSuite) observe(ctx sdk.Context) {
	var heights []int64
	it := ctx.KVStore(capKey1).Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		heights = append(heights, int64(binary.BigEndian.Uint64(it.Value())))
	}
	s.NoError(it.Close())

	transientIt := ctx.TransientStore(snapshotTransientKey).Iterator(nil, nil)
	transientDirty := transientIt.Valid()
	s.NoError(transientIt.Close())

	objectIt := ctx.ObjectStore(snapshotObjectKey).Iterator(nil, nil)
	objectDirty := objectIt.Valid()
	s.NoError(objectIt.Close())

	// Loaded after the reads: a height seen before its Commit began is uncommitted.
	committing := s.commitStarted.Load()
	s.reads.Add(1)

	if len(heights) != snapshotTestKeys {
		s.violate("kv store has %d keys, want %d", len(heights), snapshotTestKeys)
		return
	}
	for _, h := range heights {
		if h != heights[0] {
			s.violate("kv store torn across blocks: %v", heights)
			break
		}
	}
	if heights[0] > committing {
		s.violate("kv store read block %d before Commit %d began", heights[0], heights[0])
	}
	if transientDirty {
		s.violate("transient store not empty while Commit %d was the latest", committing)
	}
	if objectDirty {
		s.violate("object store not empty while Commit %d was the latest", committing)
	}
}

func (s *CheckStateSnapshotSuite) violate(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.violations) < 10 {
		s.violations = append(s.violations, fmt.Sprintf(format, args...))
	}
}

func (s *CheckStateSnapshotSuite) finalizeBlock(height int64) {
	_, err := s.app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height})
	s.Require().NoError(err)
}

func (s *CheckStateSnapshotSuite) commitBlock(height int64) {
	s.finalizeBlock(height)
	s.commitStarted.Store(height)
	_, err := s.app.Commit()
	s.Require().NoError(err)
}

func (s *CheckStateSnapshotSuite) TestReadsDuringFinalizeAndCommit() {
	checkTx := func(typ abci.CheckTxType) func() {
		return func() {
			res, err := s.app.CheckTx(&abci.RequestCheckTx{Tx: s.txBytes, Type: typ})
			s.NoError(err)
			s.True(res.IsOK(), res.Log)
		}
	}

	testCases := []struct {
		name string
		read func()
	}{
		{
			name: "simulate",
			read: func() {
				_, _, err := s.app.Simulate(s.txBytes)
				s.NoError(err)
			},
		},
		{name: "check tx", read: checkTx(abci.CheckTxType_New)},
		{name: "recheck tx", read: checkTx(abci.CheckTxType_Recheck)},
		{
			name: "check state context",
			read: func() { s.observe(s.app.NewContext(true)) },
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()

			stop := make(chan struct{})
			var wg sync.WaitGroup
			wg.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
						tc.read()
					}
				}
			})

			for height := int64(2); height <= 20; height++ {
				s.finalizeBlock(height)
				// Consensus leaves a gap between FinalizeBlock and Commit; make sure
				// the reader runs inside it.
				s.waitForReads(2)
				s.commitStarted.Store(height)
				_, err := s.app.Commit()
				s.Require().NoError(err)
			}

			close(stop)
			wg.Wait()
			s.Require().Empty(s.violations)
		})
	}
}

func (s *CheckStateSnapshotSuite) waitForReads(n int64) {
	target := s.reads.Load() + n
	s.Require().Eventually(func() bool { return s.reads.Load() >= target }, 10*time.Second, 50*time.Microsecond)
}
