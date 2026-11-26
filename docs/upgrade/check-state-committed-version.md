# Check state reads the last committed version

`CheckTx`, `ReCheckTx` and `Simulate` used to read a branch of the live stores, which `FinalizeBlock` writes while they run. Check state now branches the last committed version.

## What changes for an app

* A check context no longer sees the writes of a block that is not committed yet. This covers `NewContext(true)`, `NewContextLegacy(true, ...)` and `GetContextForCheckTx`. Call `Commit` first if a test or command needs those writes.
* Transient and object stores start empty in check state. They used to show what the block in progress had written.
* Memory stores are not versioned, so check state still reads them live.
* `BaseApp.Close` drops check state. A `CheckTx` or `Simulate` that arrives after it returns an error.
* Each `Commit` opens the committed version of every store. On IAVL with 30 stores this took 53-69 µs, against 6.4 µs for a branch of the live stores.

## Limits

* A check context is not pinned. Do not keep one, or a store taken from it, across a `Commit`: a store that frees its snapshots may have closed it. `RunTx` pins the state for its own run.
* Reads are isolated only if the store answers `CacheMultiStoreWithVersion(latest)` with a snapshot. IAVL does. A store that answers with its live stores gets no isolation.
* A pin does not stop pruning. On IAVL, a `RunTx` still running `pruning-keep-recent + 1` commits after it started (3 with `pruning = "everything"`) can read a pruned node and fail with an error.
* Before the first `Commit`, check state reads the live stores: there is no committed version to branch.
* If the store cannot branch the last committed version, check state logs an error and reads the live stores until the next `Commit`.
