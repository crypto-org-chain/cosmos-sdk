---
sidebar_position: 1
---

# Application Mempool

:::note Synopsis
This section describes how the app-side mempool can be used and replaced. 
:::

Since `v0.47` the application has its own mempool to allow much more granular
block building than previous versions. This change was enabled by
[ABCI 1.0](https://github.com/cometbft/cometbft/blob/v0.37.0/spec/abci).
Notably it introduces the `PrepareProposal` and `ProcessProposal` steps of ABCI++.

:::note Pre-requisite Readings

* [BaseApp](../../learn/advanced/00-baseapp.md)
* [ABCI](../abci/00-introduction.md)

:::

## Mempool

There are countless designs that an application developer can write for a mempool, the SDK opted to provide only simple mempool implementations.
Namely, the SDK provides the following mempools:

* [No-op Mempool](#no-op-mempool)
* [Sender Nonce Mempool](#sender-nonce-mempool)
* [Priority Nonce Mempool](#priority-nonce-mempool)
* [Multi-Lane Priority Nonce Mempool](#multi-lane-priority-nonce-mempool)

By default, when `mempool.max-txs >= 0` in `app.toml`, the SDK uses the
[Multi-Lane Priority Nonce Mempool](#multi-lane-priority-nonce-mempool); when
`mempool.max-txs < 0`, it uses the [No-op Mempool](#no-op-mempool). The mempool
can also be selected from `app.toml` or replaced entirely in [`app.go`](./01-app-go-di.md).

**Selecting via `app.toml`** — `server.DefaultBaseappOptions(appOpts)` (and the
equivalent `server.MempoolBaseappOption(appOpts)`) reads the `[mempool]` section:

```toml
[mempool]
# negative => no-op mempool, 0 => unbounded, positive => bounded.
max-txs = 0
# When max-txs >= 0:
#   "" or "multi-lane-priority-nonce" (default)
#   "sender-nonce"
#   "priority-nonce"
type = "multi-lane-priority-nonce"
```

The CLI exposes the same keys as `--mempool.max-txs` and `--mempool.type`.

**Replacing in `app.go`** — instead of using the configured mempool, an
application can wire its own implementation:

```go
nonceMempool := mempool.NewSenderNonceMempool()
mempoolOpt   := baseapp.SetMempool(nonceMempool)
baseAppOptions = append(baseAppOptions, mempoolOpt)
```

### No-op Mempool

A no-op mempool is a mempool where transactions are completely discarded and ignored when BaseApp interacts with the mempool.
When this mempool is used, it is assumed that an application will rely on CometBFT's transaction ordering defined in `RequestPrepareProposal`,
which is FIFO-ordered by default.

> Note: If a NoOp mempool is used, PrepareProposal and ProcessProposal both should be aware of this as
> PrepareProposal could include transactions that could fail verification in ProcessProposal.

### Sender Nonce Mempool

The nonce mempool is a mempool that keeps transactions from an sorted by nonce in order to avoid the issues with nonces. 
It works by storing the transaction in a list sorted by the transaction nonce. When the proposer asks for transactions to be included in a block it randomly selects a sender and gets the first transaction in the list. It repeats this until the mempool is empty or the block is full. 

It is configurable with the following parameters:

#### MaxTxs

It is an integer value that sets the mempool in one of three modes, *bounded*, *unbounded*, or *disabled*.

* **negative**: Disabled, mempool does not insert new transaction and return early.
* **zero**: Unbounded mempool has no transaction limit and will never fail with `ErrMempoolTxMaxCapacity`.
* **positive**: Bounded, it fails with `ErrMempoolTxMaxCapacity` when `maxTx` value is the same as `CountTx()`

#### Seed

Set the seed for the random number generator used to select transactions from the mempool.

### Priority Nonce Mempool

The [priority nonce mempool](https://github.com/cosmos/cosmos-sdk/blob/main/types/mempool/priority_nonce_spec.md) is a mempool implementation that stores txs in a partially ordered set by 2 dimensions:

* priority
* sender-nonce (sequence number)

Internally it uses one priority ordered [skip list](https://pkg.go.dev/github.com/huandu/skiplist) and one skip list per sender ordered by sender-nonce (sequence number). When there are multiple txs from the same sender, they are not always comparable by priority to other sender txs and must be partially ordered by both sender-nonce and priority.

It is configurable with the following parameters:

#### MaxTxs

It is an integer value that sets the mempool in one of three modes, *bounded*, *unbounded*, or *disabled*.

* **negative**: Disabled, mempool does not insert new transaction and return early.
* **zero**: Unbounded mempool has no transaction limit and will never fail with `ErrMempoolTxMaxCapacity`.
* **positive**: Bounded, it fails with `ErrMempoolTxMaxCapacity` when `maxTx` value is the same as `CountTx()`

#### Callback

The priority nonce mempool provides mempool options allowing the application sets callback(s).

* **OnRead**: Set a callback to be called when a transaction is read from the mempool.
* **TxReplacement**: Sets a callback to be called when duplicated transaction nonce detected during mempool insert. Application can define a transaction replacement rule based on tx priority or certain transaction fields.

### Multi-Lane Priority Nonce Mempool

The multi-lane priority nonce mempool extends the [Priority Nonce Mempool](#priority-nonce-mempool)
to index a transaction on **every** `(signer, nonce)` lane returned by the
configured `SignerExtractor`, not only the first signer. This matches the
semantics required when a single transaction affects multiple sender nonces
(for example, an Ethermint envelope containing multiple `MsgEthereumTx`).

This is the default mempool when `mempool.max-txs >= 0` and `mempool.type` is
unset or set to `multi-lane-priority-nonce`.

#### Behavior

* **Insert**: every `(signer, nonce)` lane derived from `SignerExtractor` is
  recorded. A transaction containing two lanes for the same `(signer, nonce)`
  is rejected as duplicate. If any lane already has a transaction, the
  configured `TxReplacement` callback is consulted on each conflicting lane;
  failure on any single lane rejects the new transaction and leaves all
  existing entries intact.
* **Capacity**: when `MaxTx > 0`, the capacity check happens **after** lane
  conflict detection, so a replacement that frees existing entries can succeed
  even when the mempool is at capacity.
* **Remove**: removes every lane the transaction occupies in a single
  operation; partial removals are not possible.
* **Select**: the iterator yields a transaction only when **all** of its lanes
  are at the ready position of their respective sender indices. Multi-lane
  transactions are emitted exactly once. A higher-priority transaction blocked
  by a lower-nonce dependency on a non-anchor lane is deferred (not removed)
  and revisited in priority order as cursors advance, so block construction
  remains O(N) over the indexed lanes.

#### Configuration

Constructed via `mempool.NewMultiLanePriorityMempool(cfg)` and accepts the
same `PriorityNonceMempoolConfig` (`TxPriority`, `MaxTx`, `SignerExtractor`,
`TxReplacement`, `OnRead`) as the priority nonce mempool — see its [MaxTxs](#maxtxs-1)
and [Callback](#callback) sections.

#### When to use

Use the multi-lane variant when:

* the application uses a non-default `SignerExtractor` that returns multiple
  signers per transaction (custom protocols, EVM compatibility), or
* lane-aware capacity / replacement semantics across all signers are required.

The original [Priority Nonce Mempool](#priority-nonce-mempool) only indexes
the first signer's lane and remains available via `mempool.type = "priority-nonce"`
for chains that prefer the historical behavior.

More information on the SDK mempool implementation can be found in the [godocs](https://pkg.go.dev/github.com/cosmos/cosmos-sdk/types/mempool).
