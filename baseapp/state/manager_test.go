package state

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/suite"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp/config"
	"github.com/cosmos/cosmos-sdk/store/v2/cachemulti"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type closableBranch struct {
	storetypes.MultiStore
	closed atomic.Int32
}

func (b *closableBranch) Write() {}

func (b *closableBranch) Close() error {
	b.closed.Add(1)
	return nil
}

type fakeCommitMultiStore struct {
	storetypes.CommitMultiStore

	version   int64
	branchErr error

	mu       sync.Mutex
	branches []*closableBranch
}

func (f *fakeCommitMultiStore) LatestVersion() int64 { return f.version }

func (f *fakeCommitMultiStore) CacheMultiStore() storetypes.CacheMultiStore {
	return cachemulti.NewStore(nil)
}

func (f *fakeCommitMultiStore) CacheMultiStoreWithVersion(int64) (storetypes.CacheMultiStore, error) {
	if f.branchErr != nil {
		return nil, f.branchErr
	}
	branch := &closableBranch{MultiStore: cachemulti.NewStore(nil)}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branches = append(f.branches, branch)
	return branch, nil
}

func (f *fakeCommitMultiStore) closedCounts() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := make([]int32, len(f.branches))
	for i, branch := range f.branches {
		counts[i] = branch.closed.Load()
	}
	return counts
}

type ManagerSuite struct {
	suite.Suite

	mgr *Manager
	cms *fakeCommitMultiStore
}

func TestManagerSuite(t *testing.T) {
	suite.Run(t, new(ManagerSuite))
}

func (s *ManagerSuite) SetupTest() {
	s.mgr = NewManager(config.GasConfig{})
	s.cms = &fakeCommitMultiStore{version: 1}
}

func (s *ManagerSuite) setCheckState() {
	s.mgr.SetState(sdk.ExecModeCheck, s.cms, cmtproto.Header{}, log.NewNopLogger(), storetypes.StreamingManager{})
}

func (s *ManagerSuite) TestCheckStateBranchLifetime() {
	replace := func() { s.setCheckState() }
	clear := func() { s.mgr.ClearState(sdk.ExecModeCheck) }

	testCases := []struct {
		name     string
		malleate func()
		readers  int
		retire   func()
		// expBranches is how many closable branches the first check state opened.
		expBranches int
	}{
		{name: "replaced with no reader", retire: replace, expBranches: 1},
		{name: "replaced under one reader", readers: 1, retire: replace, expBranches: 1},
		{name: "replaced under two readers", readers: 2, retire: replace, expBranches: 1},
		{name: "cleared under a reader", readers: 1, retire: clear, expBranches: 1},
		{
			name:     "no committed version reads the live stores",
			malleate: func() { s.cms.version = 0 },
			readers:  1,
			retire:   clear,
		},
		{
			name:     "branch error falls back to the live stores",
			malleate: func() { s.cms.branchErr = errors.New("version pruned") },
			readers:  1,
			retire:   clear,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			if tc.malleate != nil {
				tc.malleate()
			}

			s.setCheckState()
			s.Require().Len(s.cms.branches, tc.expBranches)

			unpins := make([]func(), tc.readers)
			for i := range unpins {
				var st *State
				st, unpins[i] = s.mgr.PinState(sdk.ExecModeCheck)
				s.Require().NotNil(st)
			}

			tc.retire()

			for i, unpin := range unpins {
				if tc.expBranches > 0 {
					s.Require().Zero(s.cms.branches[0].closed.Load(), "closed with %d readers left", len(unpins)-i)
				}
				unpin()
			}
			if tc.expBranches > 0 {
				s.Require().Equal(int32(1), s.cms.branches[0].closed.Load())
			}
		})
	}
}

func (s *ManagerSuite) TestCheckStateBranchStaysOpenForConcurrentReaders() {
	s.setCheckState()

	var (
		wg           sync.WaitGroup
		stop         = make(chan struct{})
		readClosed   atomic.Int64
		readsStarted atomic.Int64
	)
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				st, unpin := s.mgr.PinState(sdk.ExecModeCheck)
				readsStarted.Add(1)
				if st.MultiStore.(*committedStore).snapshot.(*closableBranch).closed.Load() != 0 {
					readClosed.Add(1)
				}
				unpin()
			}
		})
	}

	for range 2000 {
		s.setCheckState()
	}
	close(stop)
	wg.Wait()
	s.mgr.ClearState(sdk.ExecModeCheck)

	s.Require().NotZero(readsStarted.Load())
	s.Require().Zero(readClosed.Load(), "a pinned reader saw a closed branch")
	for i, closed := range s.cms.closedCounts() {
		s.Require().Equal(int32(1), closed, "branch %d", i)
	}
}

func BenchmarkSetState(b *testing.B) {
	const (
		iavlStores      = 30
		transientStores = 1
		objectStores    = 2
		keysPerStore    = 1000
	)

	backends := []struct {
		name string
		open func(b *testing.B) dbm.DB
	}{
		{name: "memdb", open: func(*testing.B) dbm.DB { return dbm.NewMemDB() }},
		{
			name: "goleveldb",
			open: func(b *testing.B) dbm.DB {
				b.Helper()
				db, err := dbm.NewGoLevelDB("bench", b.TempDir(), nil)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = db.Close() })
				return db
			},
		},
	}
	// Check state branches the committed version; every other mode still
	// branches the live stores, which is what check state did before.
	modes := []struct {
		name string
		mode sdk.ExecMode
	}{
		{name: "live", mode: sdk.ExecModePrepareProposal},
		{name: "committed", mode: sdk.ExecModeCheck},
	}

	for _, backend := range backends {
		cms := rootmulti.NewStore(backend.open(b), log.NewNopLogger())
		var kvKeys []storetypes.StoreKey
		for i := range iavlStores {
			key := storetypes.NewKVStoreKey(fmt.Sprintf("kv%d", i))
			kvKeys = append(kvKeys, key)
			cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
		}
		for i := range transientStores {
			cms.MountStoreWithDB(storetypes.NewTransientStoreKey(fmt.Sprintf("transient%d", i)), storetypes.StoreTypeTransient, nil)
		}
		for i := range objectStores {
			cms.MountStoreWithDB(storetypes.NewObjectStoreKey(fmt.Sprintf("object%d", i)), storetypes.StoreTypeObject, nil)
		}
		if err := cms.LoadLatestVersion(); err != nil {
			b.Fatal(err)
		}
		for version := range 2 {
			for _, key := range kvKeys {
				store := cms.GetKVStore(key)
				for i := range keysPerStore {
					store.Set(fmt.Appendf(nil, "key-%d", i), fmt.Appendf(nil, "value-%d-%d", version, i))
				}
			}
			cms.Commit()
		}

		mgr := NewManager(config.GasConfig{})
		for _, mode := range modes {
			b.Run(backend.name+"/"+mode.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					mgr.SetState(mode.mode, cms, cmtproto.Header{}, log.NewNopLogger(), storetypes.StreamingManager{})
				}
			})
		}
	}
}
