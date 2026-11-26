# Check state reads the last committed version

`CheckTx`, `ReCheckTx` and `Simulate` used to read a branch of the live stores, which `FinalizeBlock` writes while they run. Check state now branches the last committed version.

## What changes for an app

* A check context no longer sees the writes of a block that is not committed yet. This covers `NewContext(true)`, `NewContextLegacy(true, ...)` and `GetContextForCheckTx`. Call `Commit` first if a test or command needs those writes.
* Transient and object stores start empty in check state. They used to show what the block in progress had written.
* Memory stores are not versioned, so check state still reads them live.
* `BaseApp.Close` drops check state. A `CheckTx` or `Simulate` that arrives after it returns an error, and the check context helpers above panic with "app is closed".
* The `PrepareCheckStater` context stays readable until the hook returns, even if `Close` runs meanwhile. Do not keep it after.
* Each `Commit` opens the committed version of every IAVL store. With 30 stores this took about 50-70 µs, against about 6 µs for a branch of the live stores.

## Limits

* A check context from `NewContext(true)`, `NewContextLegacy(true, ...)` or `GetContextForCheckTx` is not pinned. Do not keep one, or a store taken from it, across a `Commit`: a store that frees its snapshots may have closed it. `RunTx` pins the state for its own run. Code that reads check state while blocks commit uses `WithCheckContext`, which pins it until the callback returns.
* Reads are isolated only if the store answers `CacheMultiStoreWithVersion(latest)` with a snapshot. IAVL does. A store that answers with its live stores gets no isolation, and the SDK cannot detect it: test it in the app.
* A pin does not stop pruning. On IAVL, a `RunTx` still running `pruning-keep-recent + 1` commits after it started (3 with `pruning = "everything"`) can read a pruned node and fail with an error.
* While the store has no committed version, check state reads the live stores: there is nothing to branch.
* If the store cannot branch the last committed version, check state logs an error and reads the live stores until the next `Commit`. Each time adds one to the `check_state.live_fallback.count` metric of the `baseapp` instrument: alert on it.
