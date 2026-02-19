# Architecture: Tiered Lock Rewards (Tier Logic)

## 1. Overview

### 1.1 Goal

Allow users to **lock tokens into a tier** (e.g. 1, 2, or 5 years). Locked tokens are **internal to the tier mechanism**: they can only be delegated, undelegated, or redelegated **within** the tier module (like internal liquid-stake tokens that cannot be used outside this system). Users receive:

- **Base rewards** from normal staking (block rewards, fees) when their tier-locked tokens are delegated to validators.
- **Bonus rewards** as a **fixed APY** on the locked amount, paid from an **external rewards pool**.

The tier module holds the locked tokens and is the delegator in `x/staking`; users operate via tier-specific messages (TierDelegate, TierUndelegate, TierRedelegate). **Add to position:** the owner can **add tokens to an existing position** (same `position_id`) as long as exit has **not** been triggered. **Withdraw from tier:** the user can **trigger exit at any time**. Once they trigger exit, an **exit commitment** starts (wait X years, depending on tier). **Once that commitment has elapsed**, no more bonus is paid and the user can claim their tokens (after unbonding if delegated).

### 1.2 Non-Goals

- Changing how base delegation rewards are computed or distributed.
- Replacing or duplicating `x/distribution`; we integrate with it.
- Managing inflation or chain-level reward issuance; the external pool is filled by external logic (governance, grants, another module).

---

## 2. Concepts

| Term | Description |
|------|-------------|
| **Tier** | A lock level defined by an **exit commitment duration** (e.g. 1y, 2y, 5y wait after user triggers exit) and a **fixed bonus APY** (e.g. 0.10 = 10% per year on the locked amount). |
| **Tier-locked tokens** | Tokens sent to the tier module when locking; they are **internal** to the mechanism. They can only be delegated/undelegated/redelegated via tier messages (internal liquid-stake style). They cannot be used externally (no transfer out except via withdraw from tier). |
| **Lock** | User sends tokens to the tier module and receives a **tier position**. The locked amount earns base (staking) rewards when delegated and a **fixed APY** bonus from the pool. User can trigger exit at any time. The owner can **add** to an existing position (same tier) as long as exit has not been triggered. |
| **Base rewards** | Staking rewards from `x/distribution` when tier-locked tokens are delegated to validators (tier module is the delegator). |
| **Bonus rewards** | **Fixed APY** on the locked (or delegated) amount: `accrued_bonus = amount × BonusAPY × (time_elapsed / 1 year)`, paid from the **tier rewards pool**. |
| **Tier rewards pool** | Module account (or external keeper) holding coins used only to pay APY bonus. Filled by governance, grants, or another module. |
| **Tier position** | A record stored in module state: unique position ID, owner, tier_id, **amount_locked**, optional exit triggered time and exit unlock time, optional validator and **delegated shares** (if delegated). A position **cannot be broken down**: the full amount is delegated to a single validator as a whole (all-or-nothing). |
| **Withdraw from tier** | Two steps: (1) **Trigger exit** — user can trigger at any time. This starts the **exit commitment** (wait X years, depending on tier). (2) **Claim** — once the exit commitment has elapsed, no more bonus; user can claim tokens (after unbonding if delegated) and position is closed. |

---

## 3. High-Level Design

- **New module**: `x/tieredrewards` (or `x/lockrewards`).
- **Tier-locked tokens**: Users **lock tokens** into the tier module (transfer to module account). Those tokens are **internal liquid-stake style**: they can only be **delegated**, **undelegated**, or **redelegated** via the tier module’s own messages. They cannot be used outside this mechanism (no external LST).
- **Delegator in staking**: The **tier module account** is the delegator for all tier-locked delegations. Each position is delegated as a whole (full amount to one validator); the module stores per-position the validator address and the **delegated shares** returned by staking when delegating that amount. Base rewards are attributed to position owners when withdrawn.
- **Bonus**: **Fixed APY** on the locked amount (or on the delegated amount), accrued over time and paid from the tier rewards pool. No multiplier on base rewards.
- **Exit commitment only**: User can trigger exit at any time. When they trigger exit, an exit commitment (X years, depending on tier) starts; once it has elapsed, no more bonus and they can claim tokens (after unbonding if delegated).
- **Dependencies**: `x/distribution`, `x/staking`, `x/bank`, `x/auth`. Tier module calls staking to delegate/undelegate/redelegate from its module account and distribution to withdraw rewards.

---

## 4. State Model

### 4.1 Params (or stored config)

```go
// TierDefinition defines a single tier.
type TierDefinition struct {
    TierId                 uint32        // e.g. 1, 2, 3
    ExitCommitmentDuration time.Duration // e.g. 5*365*24*time.Hour; after user triggers exit, they must wait this before claim; no bonus after
    BonusAPY               sdk.Dec       // e.g. 0.10 for 10% per year (fixed APY on locked amount)
}

// Params
type Params struct {
    Tiers []TierDefinition
    BonusDenoms []string  // denom(s) for bonus payouts (e.g. bond denom)
}
```

- **Fixed APY:** Bonus is not a multiplier on base rewards. It is an annual rate on the locked (or delegated) amount: `accrued_bonus = amount_locked × BonusAPY × (time_elapsed / 1 year)`, paid from the tier pool in `BonusDenoms`. Accrual can be computed per block or on each withdraw/claim using `LastAccrualTime` (or height) stored on the position.

### 4.2 Tier positions (state)

Tier locks are stored as **state records** in the module. Each record represents one lock and its delegation state. A tier position **cannot be broken down**: when delegated, the **full** `amount_locked` is delegated to a single validator (all-or-nothing).

```go
// TierPosition: one per lock. Key = position_id (unique).
type TierPosition struct {
    PositionId       uint64    // unique ID (e.g. incrementing counter)
    Owner            string   // sdk.AccAddress
    TierId           uint32
    AmountLocked     math.Int // tokens locked (bond denom); when delegated, this full amount is delegated
    CreatedAtHeight  int64
    CreatedAtTime    time.Time
    // Exit commitment: once user triggers exit, they must wait until ExitUnlockTime to claim; no bonus after that.
    ExitTriggeredAt  time.Time // zero = not triggered; when set, position is "exiting"
    ExitUnlockTime   time.Time // ExitTriggeredAt + ExitCommitmentDuration; when user can claim and bonus stops
    // Delegation state: position is either not delegated (Validator empty) or fully delegated to one validator.
    Validator        string   // validator operator address if delegated; empty if not
    DelegatedShares  sdk.Dec  // validator delegation shares for this position (see 4.2.1)
    LastBonusAccrual time.Time // for fixed APY: last time bonus was accrued
}

// Store layout:
// - PositionByID:       position_id -> TierPosition
// - PositionsByOwner:  owner || position_id -> TierPosition (for listing by owner)
// - NextPositionId:     next uint64
```

- **Lock into tier:** User sends `MsgLockTier(tier_id, amount)`. Tokens are transferred to the tier module account; module creates a `TierPosition` with `amount_locked`, no exit triggered, no validator (not yet delegated). Multiple positions per owner allowed. User can trigger exit at any time.
- **Add to position:** Owner can send `MsgAddToTierPosition(position_id, amount)` to add tokens to an **existing** position. Allowed only while exit has **not** been triggered (`ExitTriggeredAt` is zero). See §4.6 for what is updated on add.
- **Delegation:** When the user calls `MsgTierDelegate(position_id, validator)`, the module delegates the **full** `amount_locked` to that validator (position cannot be split). The staking module returns **shares** for that delegation; the tier module stores them in `DelegatedShares` (see §4.2.1). Bonus APY accrues on `amount_locked`.

#### 4.2.1 Delegated shares calculation

A tier position is always delegated as a **whole**: the full `amount_locked` is delegated in a single `staking.Delegate` call. The **delegated shares** are the shares returned by the staking module for that delegation.

- **On delegate:** The tier module calls `stakingKeeper.Delegate(ctx, tierModuleAccount, validator, position.AmountLocked)` (bond denom). The staking module converts the token amount to validator delegation shares using the validator’s current exchange rate (tokens per share) and returns the `newShares` created. The tier module sets `position.Validator = validator` and `position.DelegatedShares = newShares`.
- **On undelegate:** The tier module calls `stakingKeeper.Undelegate(ctx, tierModuleAccount, position.Validator, position.DelegatedShares)` — the **full** shares for this position. No partial undelegate; the position’s entire delegation is unbonded.
- **On redelegate:** The tier module calls `stakingKeeper.BeginRedelegate(ctx, tierModuleAccount, position.Validator, dstValidator, position.DelegatedShares)`. The full shares are moved to the destination validator; when the redelegation completes, the position’s `Validator` is updated to the destination and `DelegatedShares` is set to the **new shares** on the destination validator (staking returns the shares issued on the destination; that value is stored).

Because each position is a single delegation of a fixed amount, there is a 1:1 mapping between a tier position and one delegation entry in staking (for that validator). Base rewards for that delegation are attributed entirely to that position’s owner when they call `MsgWithdrawTierRewards`.

### 4.3 Withdraw from tier (two phases)

The user can **trigger exit at any time**. Withdraw is a **two-phase** process:

**Phase 1 — Trigger exit**

- **Message:** `MsgTriggerExitFromTier(position_id)`. Auth: signer must be `TierPosition.Owner`. Reject if already exiting (`ExitTriggeredAt != 0`). Set `ExitTriggeredAt = block_time`, `ExitUnlockTime = ExitTriggeredAt + tiers[tier_id].ExitCommitmentDuration`. Position is now in **exiting** state.
- User must wait until `block_time >= ExitUnlockTime` before they can claim. During the exit commitment period, bonus can still accrue; **once exit commitment has elapsed** (`block_time >= ExitUnlockTime`), **no more bonus** is paid for this position.

**Phase 2 — Claim tokens**

- Allowed only when `block_time >= position.ExitUnlockTime` (exit commitment elapsed). Reject otherwise.
- **Message:** `MsgWithdrawFromTier(position_id)` (or `MsgClaimFromTier(position_id)`). Auth: signer must be `TierPosition.Owner`. If position is delegated, require no active delegation and no unbonding entries (user must have undelegated and waited for unbonding). Transfer `amount_locked` from tier module to owner, delete position, emit event.

So: **lock → user may trigger exit anytime → wait X years (exit commitment, per tier) → no more bonus → claim tokens**. Optional `MsgClaimExpiredTier(position_id)` for positions that have reached `ExitUnlockTime` (same as claim above).

### 4.4 Tier rewards pool

- **Option 1:** Module account `tieredrewards` (or `tier_reward_pool`). External logic sends coins to this account (e.g. via `MsgFundTierPool` with authority, or from another module).  
- **Option 2:** External keeper interface (like distribution’s `ExternalCommunityPoolKeeper`): the tier module calls `WithdrawFromTierPool(ctx, amount)` and the external keeper moves coins to the delegator.  

Recommendation: **Module account** for simplicity; an optional **external funder** can be allowed via authority-restricted `MsgFundTierPool`.

### 4.5 How tier rewards are tracked and calculated

Tier rewards consist of **base rewards** (from staking/distribution) and **bonus rewards** (fixed APY from the tier pool). They are tracked and calculated as follows.

#### Base rewards (staking)

- **Where they are tracked:** In `x/distribution`, not in the tier module. The distribution module keeps:
  - **Validator historical rewards:** cumulative reward ratio per validator per period (updated when a period is closed and when rewards are allocated).
  - **Delegator starting info:** per (delegator, validator) the starting period and the delegator's cumulative ratio cursor, so that delegator reward = shares × (current_ratio − starting_ratio).
- **Tier's use:** The **tier module account** is the delegator. Each tier position is one delegation (full amount to one validator), so there is a 1:1 mapping: one position ⇒ one (tier_module, validator) delegation in staking. When the owner calls `MsgWithdrawTierRewards(position_id)`, the tier module calls `distribution.WithdrawDelegationRewards(ctx, tier_module_account, position.Validator)`. Distribution computes the accrued base rewards for that delegation from its stored ratios and starting info, sends the coins to the tier module's withdraw address, and updates the delegator's starting info. The tier module then sends that full amount to the position owner (no splitting, because one position = one delegation).
- **No per-position base state in tier:** The tier module does not duplicate distribution's reward state; it only stores which validator (and shares) each position has, so it knows which delegation to withdraw for when the user withdraws rewards.

#### Bonus rewards (fixed APY)

- **Where they are tracked:** In the tier module, **per position**:
  - **`LastBonusAccrual`** (time): the last time up to which bonus has been accrued (and paid or committed). Bonus for the interval from `LastBonusAccrual` to the accrual end time is computed and paid when the user withdraws; then `LastBonusAccrual` is set to that end time.
- **How they are calculated:** When paying tier rewards (e.g. in `MsgWithdrawTierRewards`):
  1. **Accrual end time:** `accrual_end = block_time`. If the position is exiting (`ExitTriggeredAt` set) and `block_time > ExitUnlockTime`, set `accrual_end = ExitUnlockTime` so no bonus is paid for time after the exit commitment has elapsed.
  2. **Time elapsed:** `time_elapsed = accrual_end − position.LastBonusAccrual` (duration in seconds or same unit as the tier's APY period).
  3. **Accrued bonus:**  
     `accrued_bonus = position.AmountLocked × tier.BonusAPY × (time_elapsed / 1 year)`  
     in the tier's bonus denom(s). The result is capped to the tier pool's available balance so the module never sends more than it holds.
  4. **Pay and update:** Send `accrued_bonus` from the tier rewards pool to the position owner. Set `position.LastBonusAccrual = accrual_end`.
- **When accrual starts:** Set when the position is created (e.g. `LastBonusAccrual = block_time` at lock) so bonus can accrue from lock time even before delegation (or from first delegate if the design prefers).
- **When accrual stops:** For positions that have triggered exit, bonus stops at `ExitUnlockTime` (no bonus for time after the exit commitment has elapsed). The calculation above enforces this by capping `accrual_end` at `ExitUnlockTime`.

#### Summary

| Reward type | Tracked in | Tier module state used | When calculated |
|------------|------------|------------------------|------------------|
| Base       | `x/distribution` (validator historical rewards, delegator starting info) | `position.Validator` (and 1:1 delegation) | On `MsgWithdrawTierRewards`: call distribution withdraw for (tier_module, position.Validator); forward full amount to owner. |
| Bonus      | Tier module: `position.LastBonusAccrual` | `LastBonusAccrual`, `AmountLocked`, tier `BonusAPY`, `ExitUnlockTime` if exiting | On withdraw: `accrued = AmountLocked × BonusAPY × (accrual_end − LastBonusAccrual) / 1 year`; cap to pool; pay to owner; set `LastBonusAccrual = accrual_end`. |

### 4.6 When the tier position must be updated

The tier position is a state record that must stay consistent with staking and with user actions. The following cases require **updating** the position (or deleting it).

| Trigger | What to update | Notes |
|--------|----------------|--------|
| **MsgTierDelegate** | `Validator`, `DelegatedShares` | Set to the chosen validator and the shares returned by staking. |
| **MsgAddToTierPosition** | See “Add to position” below | Allowed only when exit not triggered. Updates listed in next subsection. |
| **Unbonding completes** (after MsgTierUndelegate) | `Validator` → empty, `DelegatedShares` → 0 | When the tier module’s unbonding for this position’s shares completes, the position is no longer delegated. Clear delegation state so the position is “undelegated” and claim/withdraw logic is correct. Requires tracking which unbonding entry belongs to which position (e.g. via staking hook or EndBlocker that matches completion to position). |
| **Redelegation completes** (after MsgTierRedelegate) | `Validator` → destination validator, `DelegatedShares` → new shares on destination | Staking returns the shares issued on the destination; store them so future undelegate/withdraw uses the correct shares. |
| **MsgTriggerExitFromTier** | `ExitTriggeredAt`, `ExitUnlockTime` | Position enters “exiting” state; bonus stops at `ExitUnlockTime`. |
| **MsgWithdrawTierRewards** | `LastBonusAccrual` → accrual_end | So bonus is not double-counted for the same period. |
| **MsgTransferTierPosition** (optional) | `Owner` | Transfer ownership; no change to delegation or exit state. |
| **Slashing** | `AmountLocked` (and possibly `DelegatedShares`) | When the validator or the tier module’s delegation is slashed, the token value of the delegation drops. Update the position so **AmountLocked** reflects the **current** value of the delegation (e.g. tokens from `staking.TokensFromShares(position.Validator, position.DelegatedShares)` or equivalent). Otherwise (1) bonus APY would accrue on the pre-slash amount, and (2) on claim the module would owe `AmountLocked` but only hold the slashed amount. Implementation: use a staking hook (e.g. `AfterValidatorSlashed` / `BeforeValidatorSlashed`) or periodic reconciliation to detect slashing and update each affected position’s `AmountLocked` (and `DelegatedShares` if the chain reduces shares on slash). |
| **MsgWithdrawFromTier** (claim) | Position **deleted** | Not an update; the position is removed after tokens are sent to the owner. |
| **Validator leaves active set** (jailed, unbonding, removed) | No mandatory update | The delegation still exists in staking (the validator may be jailed, unbonding, or unbonded). The position’s `Validator` and `DelegatedShares` remain valid; the user can call **MsgTierRedelegate** to move to another validator or **MsgTierUndelegate** to unbond. No new block rewards are earned while the validator is inactive. **No automatic position update** is required. Optional: if the chain implements auto-undelegate when a validator is removed (e.g. for safety), then when that unbonding completes the position is updated as in “Unbonding completes” above. |

**Summary:** Delegation state (`Validator`, `DelegatedShares`) is updated on delegate, on unbonding completion, and on redelegation completion. Exit state is updated on trigger exit. Reward state (`LastBonusAccrual`) is updated on withdraw rewards. **Add to position** updates `AmountLocked` (and delegation state if already delegated). **Slashing** requires updating `AmountLocked` (and possibly `DelegatedShares`) so the position reflects the current value of the delegation and bonus/payout are correct. When a **validator leaves the set**, the position is not updated automatically; the user redelegates or undelegates.

#### Add to position: what is updated (MsgAddToTierPosition)

When the owner adds tokens to an existing position (`position_id`), the following are updated. **Precondition:** `ExitTriggeredAt` is zero (exit has not been triggered).

| Field / action | Update |
|----------------|--------|
| **AmountLocked** | Increase by the added `amount`: `position.AmountLocked += amount`. Tokens are transferred from the owner to the tier module before this update. |
| **Validator** | Unchanged (position remains delegated to the same validator or remains undelegated). |
| **DelegatedShares** | **If position is delegated** (`Validator != ""`): call `staking.Delegate(ctx, tier_module_account, position.Validator, amount)` to delegate the **new** tokens to the same validator. Staking returns `newShares`. Set `position.DelegatedShares += newShares`. **If position is not delegated:** no change. |
| **LastBonusAccrual** | **Option A (simple):** Leave unchanged. Bonus will accrue on the full (increased) `AmountLocked` for the period since `LastBonusAccrual`; the newly added amount is effectively credited with bonus from the last accrual time (slightly favorable to the user). **Option B (fair):** Before adding, settle bonus to now: compute and pay accrued bonus on the **current** `AmountLocked` from `LastBonusAccrual` to `block_time`, then set `LastBonusAccrual = block_time`. Then add the amount. The new tokens accrue bonus only from add-time onward. |
| **CreatedAtHeight**, **CreatedAtTime** | Unchanged (position creation time is preserved). |
| **ExitTriggeredAt**, **ExitUnlockTime** | Unchanged (must be zero / unset for add to be allowed). |
| **Owner**, **TierId**, **PositionId** | Unchanged. |

---

## 5. Message Flows

### 5.1 Lock into tier

```
User -> MsgLockTier(tier_id, amount)
  -> Validate tier_id exists; amount > 0; denom = bond denom
  -> Bank.SendCoinsFromAccountToModule(owner, tieredrewards.ModuleName, amount)
  -> position_id = NextPositionId; NextPositionId++
  -> Set TierPosition(position_id, owner, tier_id, amount_locked=amount, CreatedAtHeight, CreatedAtTime, ExitTriggeredAt=0, ExitUnlockTime=0, Validator="", DelegatedShares=0, LastBonusAccrual=now)
  -> Index by owner (PositionsByOwner)
  -> Emit event (position_id, owner, tier_id, amount)
```

Tokens are held by the tier module. Position is not yet delegated; user can call `MsgTierDelegate` next.

### 5.2 Add to existing position

```
User -> MsgAddToTierPosition(position_id, amount)
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; require position exists; require ExitTriggeredAt is zero (exit not triggered)
  -> Validate amount > 0; denom = bond denom
  -> Bank.SendCoinsFromAccountToModule(owner, tieredrewards.ModuleName, amount)
  -> position.AmountLocked += amount
  -> If position is delegated (Validator != ""):
       newShares = Staking.Delegate(ctx, tier_module_account, position.Validator, amount)
       position.DelegatedShares += newShares
  -> Optional (Option B): settle bonus to now (pay accrued on current amount, set LastBonusAccrual = block_time) before adding
  -> Save TierPosition
  -> Emit event (position_id, owner, amount_added, new_total)
```

Only the owner can add, and only while the position has not triggered exit. See §4.6 “Add to position: what is updated” for the full list of fields updated.

### 5.3 Tier delegate / undelegate / redelegate (internal only)

Tier-locked tokens can only be staked **within** the tier mechanism. The **tier module account** is the delegator in `x/staking`. A tier position **cannot be broken down**: delegation is always the **full** amount of the position.

**MsgTierDelegate(position_id, validator)**

```
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; require position not already delegated (Validator == "")
  -> Staking.Delegate(ctx, tier_module_account, validator, position.AmountLocked)
  -> Staking returns newShares (shares issued for this delegation)
  -> Update position: Validator = validator, DelegatedShares = newShares
  -> Emit event (position_id, owner, validator)
```

**MsgTierUndelegate(position_id)**

```
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; require position is delegated (Validator != "")
  -> Staking.Undelegate(ctx, tier_module_account, position.Validator, position.DelegatedShares)
  -> Unbonding is created. When unbonding completes, clear position.Validator and position.DelegatedShares (or track unbonding and update position state on completion).
  -> Emit event (position_id, owner)
```

**MsgTierRedelegate(position_id, dst_validator)**

```
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; require position is delegated (Validator != "")
  -> Staking.BeginRedelegate(ctx, tier_module_account, position.Validator, dst_validator, position.DelegatedShares)
  -> When redelegation completes: set position.Validator = dst_validator, position.DelegatedShares = newShares (shares issued on destination validator)
  -> Emit event (position_id, owner, dst_validator)
```

These tokens **cannot** be used outside the tier module (no external LST); they are internal to this mechanism.

### 5.4 Trigger exit from tier

```
User -> MsgTriggerExitFromTier(position_id)
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; reject if already exiting (ExitTriggeredAt != 0)
  -> ExitTriggeredAt = block_time; ExitUnlockTime = ExitTriggeredAt + tiers[tier_id].ExitCommitmentDuration
  -> Update TierPosition
  -> Emit event (position_id, owner, ExitUnlockTime)
```

User stays in tier (earning base + bonus) until they trigger exit. After triggering, they must wait until `ExitUnlockTime` to claim; once that time has passed, no more bonus.

### 5.5 Withdraw from tier (claim after exit commitment)

```
User -> MsgWithdrawFromTier(position_id)
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; require block_time >= position.ExitUnlockTime (exit commitment elapsed). If ExitTriggeredAt is zero, reject (must trigger exit first).
  -> If delegated: require no active delegation and no unbonding entries (user must have undelegated and unbonding completed)
  -> Bank.SendCoinsFromModuleToAccount(tier_module, owner, position.amount_locked)
  -> Delete TierPosition(position_id)
  -> Emit event (position_id, owner)
```

No bonus is paid after `ExitUnlockTime`; this message only transfers tokens and burns the position.

### 5.6 Withdraw tier rewards (base + fixed APY bonus)

See **§4.5** for how base and bonus rewards are tracked and calculated.

```
User -> MsgWithdrawTierRewards(position_id)
  -> Auth: signer == TierPosition.Owner
  -> Load TierPosition; must be delegated (Validator set)
  -> Base: distribution.WithdrawDelegationRewards(ctx, tier_module_account, position.Validator)
       (Distribution sends base rewards to tier module’s withdraw address; tier module then forwards owner’s share to position owner. If one position = one delegation, full amount goes to owner.)
  -> Bonus (fixed APY): accrual_end = now; if exiting (ExitTriggeredAt set) and now > ExitUnlockTime, accrual_end = ExitUnlockTime (no bonus after)
       accrued = amount_locked × tier.BonusAPY × (accrual_end - position.LastBonusAccrual) / 1 year
       Cap to tier pool balance; pay from pool to owner in BonusDenoms
  -> Update position.LastBonusAccrual = accrual_end
  -> Send base share + bonus to owner
  -> Emit event
```

- Base rewards: tier module is the delegator; its withdraw address can be set to itself so rewards arrive at the module, then the module attributes per position (by share of delegation) and sends to each owner when they call `WithdrawTierRewards`, or the module can use a single withdraw address per position (if the chain supports it). Simplest: one delegation per position so that a single withdraw gives one position’s base rewards to that owner.
- **Fixed APY** is accrued over time from `LastBonusAccrual`; if the position is exiting, accrual stops at `ExitUnlockTime` (no bonus after). Cap bonus to pool balance so users do not fail on insufficient pool.

### 5.7 Fund tier pool (authority or external)

```
Authority / External module -> MsgFundTierPool(amount)
  -> Bank.SendCoinsFromAccountToModule(sender, tieredrewards.ModuleName, amount)
  -> Emit event
```

Optional: restrict sender to governance or a dedicated “rewards treasury” module.

---

## 6. Queries

- **Tier params:** `TierParams` – list tier definitions and bonus denoms.
- **Tier pool:** `TierPoolBalance` – balance of the tier module account (or external pool) for bonus payouts.
- **Position by ID:** `TierPosition(position_id)` – return the full position record (owner, tier_id, amount_locked, exit_triggered_at, exit_unlock_time, validator, delegated_shares, created_at, etc.) by position ID.
- **Positions by owner:** `TierPositionsByOwner(owner, pagination)` – list all tier positions owned by an address (for wallets and UIs).
- **All positions:** `AllTierPositions(pagination)` – list all tier positions in the system (for explorers and analytics).
- **Estimate bonus:** `EstimateTierBonus(position_id)` – return estimated (base, bonus) for that position; useful for UX.

---

## 7. Integration with Staking and Distribution

- **Tier module as delegator:** The tier module account holds tier-locked tokens and is the **delegator** in `x/staking` for all tier delegations. The module calls `staking.Delegate`, `staking.Undelegate`, `staking.BeginRedelegate` with itself as delegator.
- **Base rewards:** When the tier module receives base rewards from `x/distribution`, the module attributes them to positions and sends to owners when they call `MsgWithdrawTierRewards`. Distribution withdraw address for the tier module can be the module account so rewards are received there and then forwarded.
- **Bonus:** Fixed APY is computed and paid from the tier pool on withdraw; no change to distribution logic.

---

## 8. Tier-Locked Tokens: Delegate / Undelegate / Redelegate Only via Tier

Tier-locked tokens **cannot** be delegated, undelegated, or redelegated using normal staking messages. They are **internal** to the tier mechanism:

- **MsgLockTier**, **MsgAddToTierPosition** (add to existing position while exit not triggered), **MsgTierDelegate(position_id, validator)**, **MsgTierUndelegate(position_id)**, **MsgTierRedelegate(position_id, dst_validator)** are the ways to lock or move tier-locked stake. The tier module account is always the delegator; each position is delegated as a whole (full amount to one validator).
- When a user calls **MsgTierUndelegate** or **MsgTierRedelegate**, staking runs as usual (unbonding/redelegation); distribution may run its hooks and pay base rewards to the tier module. The tier module attributes those rewards to the correct position(s). Design choice: require users to call **MsgWithdrawTierRewards** before **MsgTierUndelegate** so base + APY bonus are paid first.
- **Withdraw from tier:** User can trigger exit anytime with **MsgTriggerExitFromTier** (starts exit commitment, X years per tier); after exit commitment has elapsed, **MsgWithdrawFromTier** claims tokens (no more bonus). If delegated, must undelegate and wait for unbonding before claim.

---

## 9. Edge Cases and Rules

| Case | Behavior |
|------|----------|
| Lock expired | No bonus; base only. Position can be claimed/burned via MsgClaimExpiredTier. |
| No lock | No bonus; base only. |
| Pool empty / insufficient | Base withdrawal still succeeds; bonus (fixed APY accrual) capped to available pool balance (or zero). |
| Slashing | The position must be updated when the delegation is slashed: set **AmountLocked** (and **DelegatedShares** if the chain changes shares) to the current value of the delegation (see §4.6). Base rewards and stake are reduced by distribution/staking; bonus APY then accrues on the updated amount. |
| Validator leaves set (jailed, unbonding, removed) | The delegation still exists; the position’s `Validator` and `DelegatedShares` stay valid. No new block rewards while the validator is inactive. User should **MsgTierRedelegate** to an active validator or **MsgTierUndelegate** to unbond. No mandatory position update (see §4.6). Optional: chain may auto-undelegate when a validator is removed; then unbonding-completion update applies. |
| Multiple denoms in base | Base rewards per denom; bonus can be restricted to bond denom or configurable. |
| Trigger exit / claim | User can trigger exit **anytime**. **MsgTriggerExitFromTier** starts exit commitment (wait X years per tier); then **MsgWithdrawFromTier** claims tokens. No bonus after exit commitment elapsed. If delegated, must undelegate and wait unbonding before claim. |
| Multiple positions per owner | Each position is an independent state record; bonus APY and base attribution per position. |
| Add to position when exit triggered | **Reject.** `MsgAddToTierPosition` is allowed only when `ExitTriggeredAt` is zero. Once the user has triggered exit, no further adds to that position. |

---

## 10. Security and Invariants

- **Authority:** Only designated authority (e.g. gov) can update tier params and fund the pool.  
- **No double bonus:** Bonus is fixed APY accrued over time; tracked per position (`LastBonusAccrual`); paid on `MsgWithdrawTierRewards` (and optionally on TierUndelegate/TierRedelegate).  
- **Pool balance:** Never send more than pool balance; cap bonus payout to available balance.  
- **Tier-only delegation:** Only tier module can delegate/undelegate/redelegate tier-locked tokens; staking messages from users do not affect tier positions.  
- **Withdraw address:** Base rewards attributed to positions and sent to owners; bonus paid to position owner.

---

## 11. Optional: Transfer tier position

If tier positions are **transferable**, add `MsgTransferTierPosition(sender, new_owner, position_id)`:

- Auth: `sender` must be current `TierPosition.Owner`.
- Update `TierPosition.Owner` to `new_owner`; keep tier_id and exit state unchanged.
- The new owner receives tier bonus and base rewards for this position; the previous owner no longer does.

---

## 12. Optional: Distribution Hook for Base Attribution

Because the **tier module** is the delegator in staking, base rewards are paid to the tier module. To attribute base rewards to individual positions (each position is one delegation, so attribution is per position), the chain can:

- Use a single withdraw address (the tier module account) and attribute base rewards internally by position when users call `MsgWithdrawTierRewards`, or  
- Extend `x/distribution` with a hook (e.g. `AfterDelegationRewardsWithdrawn`) so the tier module can attribute and forward base to position owners when distribution pays out.  

Bonus is **fixed APY** from the tier pool, not a multiplier on base; the hook would only affect how **base** rewards are attributed, not the bonus formula.

---

## 13. Module Layout

```
x/tieredrewards/
  keeper/
    keeper.go         # Keeper, params, pool balance, position store
    msg_server.go     # MsgLockTier, MsgAddToTierPosition, MsgTierDelegate, MsgTierUndelegate, MsgTierRedelegate, MsgTriggerExitFromTier, MsgWithdrawFromTier, MsgWithdrawTierRewards, MsgFundTierPool, MsgClaimExpiredTier, [MsgTransferTierPosition]
    query_server.go   # PositionByID, PositionsByOwner, AllTierPositions, params, pool
    position.go       # TierPosition CRUD, attribution
  types/
    keys.go
    params.go
    genesis.go
    messages.go
  module.go
  abci.go             # no-op or future pool refill logic
```

**Expected keepers:**

- `stakingKeeper`: `Delegate`, `Undelegate`, `BeginRedelegate` (tier module account as delegator).  
- `distributionKeeper`: withdraw base rewards to module; optional hook for attribution.  
- `bankKeeper`: send base + bonus to position owners; receive lock and pool funding.  
- `authKeeper`: module account, address codec.

---

## 14. Summary

- **Tier = lock duration + fixed bonus APY.** Tier-locked tokens are **internal** to the tier mechanism: only **MsgTierDelegate**, **MsgTierUndelegate**, **MsgTierRedelegate** move stake; the tier module account is the delegator.  
- **Tier positions are state records:** each lock is a `TierPosition` (position_id, owner, tier_id, amount_locked, created_at, exit_triggered_at, exit_unlock_time, validator, delegated_shares, last_bonus_accrual). A position cannot be broken down; the full amount is delegated to one validator. The owner can **add** to an existing position via `MsgAddToTierPosition` as long as exit has not been triggered. Store: `PositionByID`, `PositionsByOwner`, `AllTierPositions`.  
- **Withdraw from tier:** User can trigger exit **at any time**. **MsgTriggerExitFromTier** starts the exit commitment (wait X years, per tier); once it has elapsed, **no more bonus** and **MsgWithdrawFromTier** claims tokens (after unbonding if delegated). Optional `MsgClaimExpiredTier` for positions past `ExitUnlockTime`.  
- **Bonus = fixed APY** on locked amount, accrued over time and paid from a **tier rewards pool** when user calls `MsgWithdrawTierRewards` (and optionally on TierUndelegate/TierRedelegate).  
- **Integration:** Tier module holds tokens and delegates via staking; base rewards received by module and attributed to positions; bonus from pool.  
- **External pool:** Filled by governance or another module; tier module pays fixed-APY bonus from it.  
- **Safety:** Cap bonus to pool balance; only tier module can delegate/undelegate/redelegate tier-locked tokens; single authority for params and funding.
