package state

import (
	"bytes"
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
	baseapptestutil "github.com/cosmos/cosmos-sdk/baseapp/testutil"
	"github.com/cosmos/cosmos-sdk/store/v2/cache"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	committedValue = "committed"
	liveValue      = "live"
	checkValue     = "check"
)

var (
	kvKey        = storetypes.NewKVStoreKey("kv")
	transientKey = storetypes.NewTransientStoreKey("transient")
	objectKey    = storetypes.NewObjectStoreKey("object")
	memoryKey    = storetypes.NewMemoryStoreKey("memory")

	testKey = []byte("key")
)

type ManagerSuite struct {
	suite.Suite

	mgr  *Manager
	cms  *baseapptestutil.ClosableCommitMultiStore
	logs bytes.Buffer
}

func TestManagerSuite(t *testing.T) {
	suite.Run(t, new(ManagerSuite))
}

// SetupTest leaves the store at version 1 with a block in progress on top.
func (s *ManagerSuite) SetupTest() {
	s.mgr = NewManager(config.GasConfig{})
	s.logs.Reset()
	s.openStore()
	s.cms.GetKVStore(kvKey).Set(testKey, []byte(committedValue))
	s.cms.Commit()
	s.writeLive()
}

func (s *ManagerSuite) openStore() {
	cms := rootmulti.NewStore(dbm.NewMemDB(), log.NewNopLogger())
	cms.MountStoreWithDB(kvKey, storetypes.StoreTypeIAVL, nil)
	cms.MountStoreWithDB(transientKey, storetypes.StoreTypeTransient, nil)
	cms.MountStoreWithDB(objectKey, storetypes.StoreTypeObject, nil)
	cms.MountStoreWithDB(memoryKey, storetypes.StoreTypeMemory, nil)
	s.Require().NoError(cms.LoadLatestVersion())
	s.cms = &baseapptestutil.ClosableCommitMultiStore{CommitMultiStore: cms}
}

func (s *ManagerSuite) writeLive() {
	s.cms.GetKVStore(kvKey).Set(testKey, []byte(liveValue))
	s.cms.GetKVStore(transientKey).Set(testKey, []byte(liveValue))
	s.cms.GetObjKVStore(objectKey).Set(testKey, liveValue)
	s.cms.GetKVStore(memoryKey).Set(testKey, []byte(liveValue))
}

func (s *ManagerSuite) setCheckState() {
	logger := log.NewLogger(&s.logs, log.ColorOption(false))
	s.mgr.SetState(sdk.ExecModeCheck, s.cms, cmtproto.Header{}, logger, storetypes.StreamingManager{})
}

func readKV(ms storetypes.MultiStore, key storetypes.StoreKey) string {
	return string(ms.GetKVStore(key).Get(testKey))
}

func (s *ManagerSuite) TestCheckStateBranchLifetime() {
	replaceState := func() { s.setCheckState() }
	clearState := func() { s.mgr.ClearState(sdk.ExecModeCheck) }

	testCases := []struct {
		name     string
		malleate func()
		readers  int
		retire   func()
		// expBranches is how many closable branches the first check state opened.
		expBranches int
		expRead     string
		expLog      string
	}{
		{name: "replaced with no reader", retire: replaceState, expBranches: 1, expRead: committedValue},
		{name: "replaced under one reader", readers: 1, retire: replaceState, expBranches: 1, expRead: committedValue},
		{name: "replaced under two readers", readers: 2, retire: replaceState, expBranches: 1, expRead: committedValue},
		{name: "cleared with no reader", retire: clearState, expBranches: 1, expRead: committedValue},
		{name: "cleared under a reader", readers: 1, retire: clearState, expBranches: 1, expRead: committedValue},
		{
			name:     "branch with nothing to close stays readable",
			malleate: func() { s.cms.UnclosableBranches = true },
			readers:  1,
			retire:   replaceState,
			expRead:  committedValue,
		},
		{
			name: "no committed version reads the live stores",
			malleate: func() {
				s.openStore()
				s.writeLive()
			},
			readers: 1,
			retire:  clearState,
			expRead: liveValue,
		},
		{
			name:     "branch error falls back to the live stores",
			malleate: func() { s.cms.BranchErr = errors.New("version pruned") },
			readers:  1,
			retire:   replaceState,
			expRead:  liveValue,
			expLog:   "check state cannot branch the committed version and reads the live stores until the next commit",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			if tc.malleate != nil {
				tc.malleate()
			}
			closedUnderLock := false
			s.cms.OnClose = func() {
				if !s.mgr.stateMut.TryLock() {
					closedUnderLock = true
					return
				}
				s.mgr.stateMut.Unlock()
			}

			s.setCheckState()
			s.Require().Len(s.cms.ClosedCounts(), tc.expBranches)
			if tc.expLog == "" {
				s.Require().Empty(s.logs.String())
			} else {
				s.Require().Contains(s.logs.String(), tc.expLog)
				s.Require().Contains(s.logs.String(), s.cms.BranchErr.Error())
			}

			unpinned := s.mgr.GetState(sdk.ExecModeCheck)
			unpins := make([]func(), tc.readers)
			for i := range unpins {
				var st *State
				st, unpins[i] = s.mgr.PinState(sdk.ExecModeCheck)
				s.Require().Same(unpinned, st)
			}

			tc.retire()

			for i, unpin := range unpins {
				if tc.expBranches > 0 {
					s.Require().Zero(s.cms.ClosedCounts()[0], "closed with %d readers left", len(unpins)-i)
				}
				s.Require().Equal(tc.expRead, readKV(unpinned.MultiStore, kvKey), "read with %d readers left", len(unpins)-i)
				unpin()
			}

			if tc.expBranches == 0 {
				s.Require().Equal(tc.expRead, readKV(unpinned.MultiStore, kvKey))
				return
			}
			s.Require().Equal(int32(1), s.cms.ClosedCounts()[0])
			s.Require().False(closedUnderLock, "a store close under the state lock blocks readers")
			s.Require().PanicsWithValue(releasedPanic, func() { readKV(unpinned.MultiStore, kvKey) })
		})
	}
}

func (s *ManagerSuite) TestOtherModesKeepCheckState() {
	testCases := []struct {
		name     string
		mode     sdk.ExecMode
		expPanic bool
	}{
		{name: "prepare proposal", mode: sdk.ExecModePrepareProposal},
		{name: "process proposal", mode: sdk.ExecModeProcessProposal},
		{name: "finalize", mode: sdk.ExecModeFinalize},
		{name: "recheck is not a state of its own", mode: sdk.ExecModeReCheck, expPanic: true},
		{name: "simulate is not a state of its own", mode: sdk.ExecModeSimulate, expPanic: true},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setCheckState()
			checkState := s.mgr.GetState(sdk.ExecModeCheck)

			setMode := func() {
				s.mgr.SetState(tc.mode, s.cms, cmtproto.Header{}, log.NewNopLogger(), storetypes.StreamingManager{})
			}
			clearMode := func() { s.mgr.ClearState(tc.mode) }
			if tc.expPanic {
				s.Require().PanicsWithValue(fmt.Sprintf("invalid runTxMode for setState: %d", tc.mode), setMode)
				s.Require().PanicsWithValue(fmt.Sprintf("invalid runTxMode for clearState: %d", tc.mode), clearMode)
			} else {
				setMode()
				s.Require().NotNil(s.mgr.GetState(tc.mode))
				s.Require().NotSame(checkState, s.mgr.GetState(tc.mode))
				clearMode()
				s.Require().Nil(s.mgr.GetState(tc.mode))
			}

			s.Require().Same(checkState, s.mgr.GetState(sdk.ExecModeCheck))
			s.Require().Equal([]int32{0}, s.cms.ClosedCounts())
		})
	}
}

func (s *ManagerSuite) TestCheckStateReleaseGuard() {
	testCases := []struct {
		name string
		// open returns a read that runs after the check state is released.
		open     func(ms storetypes.MultiStore) func() string
		expPanic bool
	}{
		{
			name: "new store lookup panics",
			open: func(ms storetypes.MultiStore) func() string {
				return func() string { return readKV(ms, kvKey) }
			},
			expPanic: true,
		},
		{
			name: "branch panics on a store it had not opened",
			open: func(ms storetypes.MultiStore) func() string {
				branch := ms.CacheMultiStore()
				return func() string { return readKV(branch, kvKey) }
			},
			expPanic: true,
		},
		// Known limit, see committedStore.released: these read a value only because
		// the fixture's Close frees nothing.
		{
			name: "store looked up before release is not guarded",
			open: func(ms storetypes.MultiStore) func() string {
				store := ms.GetKVStore(kvKey)
				return func() string { return string(store.Get(testKey)) }
			},
		},
		{
			name: "branch store opened before release is not guarded",
			open: func(ms storetypes.MultiStore) func() string {
				branch := ms.CacheMultiStore()
				branch.GetKVStore(kvKey)
				return func() string { return readKV(branch, kvKey) }
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setCheckState()
			read := tc.open(s.mgr.GetState(sdk.ExecModeCheck).MultiStore)

			s.mgr.ClearState(sdk.ExecModeCheck)

			if tc.expPanic {
				s.Require().PanicsWithValue(releasedPanic, func() { read() })
				return
			}
			s.Require().Equal(committedValue, read())
		})
	}
}

func (s *ManagerSuite) TestCheckStateWritePanics() {
	s.setCheckState()

	s.Require().PanicsWithValue(writePanic, s.mgr.GetState(sdk.ExecModeCheck).MultiStore.Write)
}

func (s *ManagerSuite) TestCheckStateStores() {
	checkStore := func() storetypes.MultiStore { return s.mgr.GetState(sdk.ExecModeCheck).MultiStore }
	// Writes the way a tx does: into a branch of the check state, then flushed.
	writeTx := func() {
		branch := checkStore().CacheMultiStore()
		branch.GetKVStore(transientKey).Set(testKey, []byte(checkValue))
		branch.GetObjKVStore(objectKey).Set(testKey, checkValue)
		branch.Write()
	}
	nextBlock := func() {
		s.cms.Commit()
		s.setCheckState()
	}
	readObject := func() any { return checkStore().GetObjKVStore(objectKey).Get(testKey) }

	testCases := []struct {
		name     string
		malleate func()
		read     func() any
		exp      any
	}{
		{
			name: "kv store reads the committed version",
			read: func() any { return readKV(checkStore(), kvKey) },
			exp:  committedValue,
		},
		{
			name: "transient store does not see the block in progress",
			read: func() any { return readKV(checkStore(), transientKey) },
			exp:  "",
		},
		{
			name: "object store does not see the block in progress",
			read: readObject,
			exp:  nil,
		},
		{
			name: "memory store is not versioned and reads live",
			read: func() any { return readKV(checkStore(), memoryKey) },
			exp:  liveValue,
		},
		{
			name:     "transient write is read back",
			malleate: writeTx,
			read:     func() any { return readKV(checkStore(), transientKey) },
			exp:      checkValue,
		},
		{
			name:     "object write is read back",
			malleate: writeTx,
			read:     readObject,
			exp:      checkValue,
		},
		{
			name: "transient write is dropped at the next commit",
			malleate: func() {
				writeTx()
				nextBlock()
			},
			read: func() any { return readKV(checkStore(), transientKey) },
			exp:  "",
		},
		{
			name: "object write is dropped at the next commit",
			malleate: func() {
				writeTx()
				nextBlock()
			},
			read: readObject,
			exp:  nil,
		},
		{
			name:     "check state writes do not reach the live stores",
			malleate: writeTx,
			read: func() any {
				return []any{readKV(s.cms, transientKey), s.cms.GetObjKVStore(objectKey).Get(testKey)}
			},
			exp: []any{liveValue, liveValue},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setCheckState()
			if tc.malleate != nil {
				tc.malleate()
			}

			s.Require().Equal(tc.exp, tc.read())
		})
	}
}

func (s *ManagerSuite) TestCheckStateBranchStaysOpenForConcurrentReaders() {
	s.setCheckState()

	var (
		wg           sync.WaitGroup
		stop         = make(chan struct{})
		readClosed   atomic.Int64
		misreads     atomic.Int64
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
				if st.MultiStore.(*committedStore).snapshot.(*baseapptestutil.ClosableBranch).Closed() != 0 {
					readClosed.Add(1)
				}
				if readKV(st.MultiStore, kvKey) != committedValue {
					misreads.Add(1)
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
	s.Require().Zero(misreads.Load(), "a pinned reader did not read the committed version")
	for i, closed := range s.cms.ClosedCounts() {
		s.Require().Equal(int32(1), closed, "branch %d", i)
	}
}

const (
	benchIAVLStores   = 30
	benchKeysPerStore = 1000
)

var (
	benchBackends = []struct {
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
	benchModes = []struct {
		name string
		mode sdk.ExecMode
	}{
		{name: "live", mode: sdk.ExecModePrepareProposal},
		{name: "committed", mode: sdk.ExecModeCheck},
	}
)

func benchKey(i int) []byte {
	return fmt.Appendf(nil, "key-%d", i)
}

// benchStore commits two versions of benchKeysPerStore keys to each IAVL store.
func benchStore(b *testing.B, db dbm.DB, interBlockCache bool) (storetypes.CommitMultiStore, []storetypes.StoreKey) {
	b.Helper()
	const (
		transientStores = 1
		objectStores    = 2
	)

	cms := rootmulti.NewStore(db, log.NewNopLogger())
	if interBlockCache {
		cms.SetInterBlockCache(cache.NewCommitKVStoreCacheManager(cache.DefaultCommitKVStoreCacheSize))
	}
	var kvKeys []storetypes.StoreKey
	for i := range benchIAVLStores {
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
			for i := range benchKeysPerStore {
				store.Set(benchKey(i), fmt.Appendf(nil, "value-%d-%d", version, i))
			}
		}
		cms.Commit()
	}
	return cms, kvKeys
}

func BenchmarkSetState(b *testing.B) {
	for _, backend := range benchBackends {
		cms, _ := benchStore(b, backend.open(b), false)
		mgr := NewManager(config.GasConfig{})
		for _, mode := range benchModes {
			b.Run(backend.name+"/"+mode.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					mgr.SetState(mode.mode, cms, cmtproto.Header{}, log.NewNopLogger(), storetypes.StreamingManager{})
				}
			})
		}
	}
}

// A state caches what it reads, so only the first read of a key in each block
// reaches the store, and the committed version is read without the inter-block cache.
func BenchmarkStateFirstRead(b *testing.B) {
	const readsPerBlock = 100

	keys := make([][]byte, readsPerBlock)
	for i := range keys {
		keys[i] = benchKey(i * benchKeysPerStore / readsPerBlock)
	}

	for _, backend := range benchBackends {
		for _, interBlockCache := range []bool{false, true} {
			cms, kvKeys := benchStore(b, backend.open(b), interBlockCache)
			mgr := NewManager(config.GasConfig{})
			for _, mode := range benchModes {
				b.Run(fmt.Sprintf("%s/inter-block-cache=%t/%s", backend.name, interBlockCache, mode.name), func(b *testing.B) {
					for b.Loop() {
						b.StopTimer()
						mgr.SetState(mode.mode, cms, cmtproto.Header{}, log.NewNopLogger(), storetypes.StreamingManager{})
						store := mgr.GetState(mode.mode).MultiStore.GetKVStore(kvKeys[0])
						b.StartTimer()

						for _, key := range keys {
							if store.Get(key) == nil {
								b.Fatal("missing key")
							}
						}
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*readsPerBlock), "ns/read")
				})
			}
		}
	}
}
