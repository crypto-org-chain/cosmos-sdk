package baseapp_test

import (
	"encoding/binary"
	"errors"
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
	cms     *baseapptestutil.ClosableCommitMultiStore
	txBytes []byte

	// antePanic, when set, is what the ante handler panics with.
	antePanic any
	// prepareCheckState, when set, runs as the app's PrepareCheckStater.
	prepareCheckState func(sdk.Context)

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
	s.setup(false)
}

// setup leaves the app at block 1; unclosableBranches gives check state a branch
// with nothing to close, which is all an unpinned reader can use across a Commit.
func (s *CheckStateSnapshotSuite) setup(unclosableBranches bool) {
	s.commitStarted.Store(0)
	s.reads.Store(0)
	s.violations = nil
	s.antePanic = nil
	s.prepareCheckState = nil

	bas := NewBaseAppSuite(s.T(), func(app *baseapp.BaseApp) {
		app.SetPrepareCheckStater(func(ctx sdk.Context) {
			if s.prepareCheckState != nil {
				s.prepareCheckState(ctx)
			}
		})
		s.cms = &baseapptestutil.ClosableCommitMultiStore{CommitMultiStore: app.CommitMultiStore(), UnclosableBranches: unclosableBranches}
		app.SetCMS(s.cms)
		app.MountStores(snapshotTransientKey, snapshotObjectKey)
		app.SetBeginBlocker(func(ctx sdk.Context) (sdk.BeginBlock, error) {
			writeBlockHeight(ctx)
			return sdk.BeginBlock{}, nil
		})
		app.SetAnteHandler(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			if s.antePanic != nil {
				panic(s.antePanic)
			}
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

// checkTx returns the error text of the response, empty when the tx passed.
func (s *CheckStateSnapshotSuite) checkTx(typ abci.CheckTxType) func() string {
	return func() string {
		res, err := s.app.CheckTx(&abci.RequestCheckTx{Tx: s.txBytes, Type: typ})
		s.NoError(err)
		if res.IsOK() {
			return ""
		}
		return res.Log
	}
}

func (s *CheckStateSnapshotSuite) simulate() string {
	if _, _, err := s.app.Simulate(s.txBytes); err != nil {
		return err.Error()
	}
	return ""
}

func (s *CheckStateSnapshotSuite) closeApp() {
	s.Require().NoError(s.app.Close())
}

func (s *CheckStateSnapshotSuite) TestReadsDuringFinalizeAndCommit() {
	const lastHeight = 20

	testCases := []struct {
		name string
		read func()
		// unpinned readers do not go through RunTx.
		unpinned bool
	}{
		{name: "simulate", read: func() { s.Empty(s.simulate()) }},
		{name: "check tx", read: func() { s.Empty(s.checkTx(abci.CheckTxType_New)()) }},
		{name: "recheck tx", read: func() { s.Empty(s.checkTx(abci.CheckTxType_Recheck)()) }},
		{
			name:     "check state context",
			read:     func() { s.observe(s.app.NewContext(true)) },
			unpinned: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.setup(tc.unpinned)

			stop := make(chan struct{})
			var wg sync.WaitGroup
			// Deferred too: a failed assertion must not leave the reader running past the test.
			stopReader := sync.OnceFunc(func() {
				close(stop)
				wg.Wait()
			})
			defer stopReader()
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

			for height := int64(2); height <= lastHeight; height++ {
				s.finalizeBlock(height)
				// Consensus leaves a gap between FinalizeBlock and Commit; make sure
				// the reader runs inside it.
				s.waitForReads(2)
				s.commitStarted.Store(height)
				_, err := s.app.Commit()
				s.Require().NoError(err)
			}

			stopReader()
			s.Require().Empty(s.violations)

			s.closeApp()
			closed := s.cms.ClosedCounts()
			if !tc.unpinned {
				s.Require().Len(closed, lastHeight)
			}
			for i, count := range closed {
				s.Require().Equal(int32(1), count, "branch %d", i)
			}
		})
	}
}

func (s *CheckStateSnapshotSuite) TestCheckStateBranchIsReleased() {
	commit := func() { s.commitBlock(2) }

	testCases := []struct {
		name      string
		antePanic any
		run       func() string
		expErr    string
		retire    func()
		// expClosed is the close count of each check state branch, oldest first.
		expClosed []int32
	}{
		{name: "check tx, then commit", run: s.checkTx(abci.CheckTxType_New), retire: commit, expClosed: []int32{1, 0}},
		{name: "recheck tx, then commit", run: s.checkTx(abci.CheckTxType_Recheck), retire: commit, expClosed: []int32{1, 0}},
		{name: "simulate, then commit", run: s.simulate, retire: commit, expClosed: []int32{1, 0}},
		{
			name:      "ante handler panics, then commit",
			antePanic: "ante handler failed",
			run:       s.checkTx(abci.CheckTxType_New),
			expErr:    "ante handler failed",
			retire:    commit,
			expClosed: []int32{1, 0},
		},
		{
			name:      "ante handler runs out of gas, then commit",
			antePanic: storetypes.ErrorOutOfGas{Descriptor: "ante handler"},
			run:       s.simulate,
			expErr:    "out of gas",
			retire:    commit,
			expClosed: []int32{1, 0},
		},
		{name: "check tx, then close", run: s.checkTx(abci.CheckTxType_New), retire: s.closeApp, expClosed: []int32{1}},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.antePanic = tc.antePanic

			errText := tc.run()
			if tc.expErr == "" {
				s.Require().Empty(errText)
			} else {
				s.Require().Contains(errText, tc.expErr)
			}
			s.Require().Equal([]int32{0}, s.cms.ClosedCounts())

			tc.retire()

			s.Require().Equal(tc.expClosed, s.cms.ClosedCounts())
		})
	}
}

func (s *CheckStateSnapshotSuite) TestPrepareCheckStaterKeepsItsBranchOpen() {
	testCases := []struct {
		name        string
		closeInHook bool
		// hookPanic, when set, is what the hook panics with after its read; the app then closes.
		hookPanic any
		// expClosed is the close count of each check state branch at the end, oldest first.
		expClosed []int32
	}{
		{name: "app stays open", expClosed: []int32{1, 0}},
		{name: "app closes during the hook", closeInHook: true, expClosed: []int32{1, 1}},
		// Close frees the new branch only if the panicking hook dropped its pin.
		{name: "hook panics, then app closes", hookPanic: "hook failed", expClosed: []int32{1, 1}},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()

			var closedInHook []int32
			s.prepareCheckState = func(ctx sdk.Context) {
				if tc.closeInHook {
					s.closeApp()
				}
				s.observe(ctx)
				closedInHook = s.cms.ClosedCounts()
				if tc.hookPanic != nil {
					panic(tc.hookPanic)
				}
			}

			if tc.hookPanic == nil {
				s.commitBlock(2)
			} else {
				s.Require().PanicsWithValue(tc.hookPanic, func() { s.commitBlock(2) })
				s.closeApp()
			}

			s.Require().Equal([]int32{1, 0}, closedInHook)
			s.Require().Empty(s.violations)
			s.Require().Equal(tc.expClosed, s.cms.ClosedCounts())
		})
	}
}

func (s *CheckStateSnapshotSuite) TestCloseDuringInitChain() {
	app := NewBaseAppSuite(s.T(), func(app *baseapp.BaseApp) {
		app.SetInitChainer(func(sdk.Context, *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
			return &abci.ResponseInitChain{}, app.Close()
		})
	}).baseApp

	_, err := app.InitChain(&abci.RequestInitChain{ConsensusParams: &cmtproto.ConsensusParams{}})
	s.Require().NoError(err)
}

func (s *CheckStateSnapshotSuite) TestLiveFallbackIsCounted() {
	testCases := []struct {
		name      string
		branchErr error
		expCount  int64
	}{
		{name: "committed version branches"},
		{name: "branch error on every commit", branchErr: errors.New("version pruned"), expCount: 2},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			count := baseapp.CountCheckStateLiveFallbacks(s.T())
			s.cms.BranchErr = tc.branchErr

			s.commitBlock(2)
			s.commitBlock(3)

			s.Require().Equal(tc.expCount, count())
		})
	}
}

func (s *CheckStateSnapshotSuite) TestTxAfterClose() {
	runTx := func(mode sdk.ExecMode) func() string {
		return func() string {
			_, _, _, err := s.app.RunTx(mode, s.txBytes, nil, -1, nil, nil)
			return err.Error()
		}
	}

	nilState := func(mode sdk.ExecMode) string {
		return fmt.Sprintf("state is nil for mode %v", mode)
	}
	const appClosed = "app is closed: no check state"

	testCases := []struct {
		name string
		run  func() string
		// expPanic is set where there is no error to return: the block modes, which have
		// no state between blocks, and the check context helpers.
		expPanic string
	}{
		{name: "check tx", run: s.checkTx(abci.CheckTxType_New)},
		{name: "recheck tx", run: s.checkTx(abci.CheckTxType_Recheck)},
		{name: "simulate", run: s.simulate},
		{
			name:     "new check context",
			run:      func() string { s.app.NewContext(true); return "" },
			expPanic: appClosed,
		},
		{
			name:     "check context for tx",
			run:      func() string { s.app.GetContextForCheckTx(s.txBytes); return "" },
			expPanic: appClosed,
		},
		{name: "finalize", run: runTx(sdk.ExecModeFinalize), expPanic: nilState(sdk.ExecModeFinalize)},
		{name: "prepare proposal", run: runTx(sdk.ExecModePrepareProposal), expPanic: nilState(sdk.ExecModePrepareProposal)},
		{name: "process proposal", run: runTx(sdk.ExecModeProcessProposal), expPanic: nilState(sdk.ExecModeProcessProposal)},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.closeApp()

			if tc.expPanic != "" {
				s.Require().PanicsWithValue(tc.expPanic, func() { tc.run() })
				return
			}
			s.Require().Contains(tc.run(), "app is closed")
		})
	}
}

func (s *CheckStateSnapshotSuite) waitForReads(n int64) {
	target := s.reads.Load() + n
	s.Require().Eventually(func() bool { return s.reads.Load() >= target }, 10*time.Second, 50*time.Microsecond)
}
