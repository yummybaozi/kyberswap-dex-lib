# Ticker DualPool integration — implementation record

Status: in progress; no deployed Ticker hook address is registered and no mainnet deployment is authorized by this work.

## Pinned inputs

- Ticker: `jaybutera/ponsfamily` commit `a8b41e44916d3d6266f8d38f2f3cd116c2379207`.
- Kyber baseline: `8a954853c26c8e7a799f810c1ec05e2da0165954`.
- Working branch: `codex/ticker-dualpool-integration`.

## What we can reuse, and what still needs work

Kyber already reconstructs DualPool's temporary liquidity positions from the bucket distribution, balances, and price. Ticker uses this same underlying design. Its extra swap rules belong in a Ticker-specific integration, without silently changing Twofold's fee behavior.

### Fees must use the output produced by DualPool

Ticker first completes the underlying swap, then subtracts two separately rounded amounts from exact-input output:

```
user output = gross output
            - floor(gross output * hookFeeBps / 10000)
            - floor(gross output * creatorTaxBps / 10000)
```

The pool loses gross output, not the smaller amount the user receives. Reverse quotes must invert this exact-input calculation, not invoke Solidity's exact-output fee branch.

The audit probe `TestTickerAudit_NaiveDualPoolPlusPonsFeeChargesZero` reproduces why simply composing the existing DualPool BeforeSwap handler and classic Pons AfterSwap handler is wrong: Kyber's ordinary AfterSwap amount excludes the output supplied through BeforeSwap deltas. The composed fee handler sees zero. At the fixture's gross output of 421126678297991910, a 1% fee should leave 416915411515011991, but naive composition leaves the entire gross amount.

### Reusing all swap math unchanged is not yet justified

The existing DualPool simulator jumps straight to the next initialized tick. Ticker's vendored v4 `Pool.swap` calls `nextInitializedTickWithinOneWord`, which also stops at uninitialized bitmap-word boundaries. Fees and token amounts round at each step; skipping a boundary can change output and the ending price.

`TestReauditWordBoundary` reproduces this using spacing 200 and a swap crossing tick -256200:

- Existing simulator output: 13428054353390492650.
- Explicit two-step calculation: 13428054105542917337.

This is a Go reproduction checked against the Solidity control flow, NOT yet an independently executed Solidity fixture. The final integration needs actual contract differential tests. Retain reusable allocation/math helpers, but accurately reproduce v4's step boundaries in the Ticker path. Review zero-liquidity gaps, overlapping positions, checked arithmetic, liquidity rounding, and post-swap inventory rather than assuming the rest is exact.

### The full-liquidity rule is separate from fees

Ticker compares immediately deliverable assets against total backing for both tokens before swapping. Any shortfall rejects the swap. Read both at the same block. A pool with 1000 USDC backing and 400 withdrawable must not be quoted as a tradable 400-USDC pool.

Within a PoolManager unlock, Ticker also subtracts unbacked claims before that comparison. A settled RPC snapshot cannot describe claims created by an earlier leg of a future route. Repeated-pool and shared-inventory routes need execution-aware tests and supported routing constraints; do not claim two snapshot getters alone solve this.

## Ordered remaining work

1. Produce independent Solidity fixtures for allocation, step boundaries, fees, inventory after teardown, and shortage reverts.
2. Expose/reuse compatible DualPool calculation code and implement Ticker's precise stepping and fee treatment.
3. Add a Ticker tracker with snapshot-consistent backing, capacity, launch configuration, price, and distribution reads.
4. Add quote purity, clone isolation, UpdateBalance, reverse quote, arithmetic-boundary, and route/claim tests.
5. Validate pool discovery and actual router transaction encoding on Monad. No invented deployment address or fabricated gas estimate.
6. Record real deployed-pool fixtures and gas once an authorized deployment exists; clearly separate those remaining checks from local validation.
7. Prepare PR-ready documentation and run repository-required formatting and targeted regression tests. Submission to Kyber requires subsequent authorization.

## Validation so far

At the pinned Kyber baseline, the three audit probes pass (they assert the current limitations, not completion of a Ticker integration):

```
go test ./pkg/liquidity-source/uniswap/v4/hooks/dualpool -run 'TestTickerAudit|TestReauditWordBoundary' -v
```

These temporary diagnostic tests will be replaced or converted into correctness regressions as the implementation is built.

## Local validation update — 2026-10-06

Implemented a separate Ticker quote calculation and an opt-in bitmap stepping path in shared DualPool math. Existing Twofold calls retain their previous path. Ticker checks full backing, subtracts explicitly supplied unbacked claims, and applies the two output fees separately.

Executed `TickerQuoteFixture` against real local Ticker hook/proxy/PoolManager contracts: eight fixtures match Go exactly for net output, both fees, ending sqrt price, and tick. These use the repository's mock strategy. Post-teardown balances are also recorded, but are NOT yet modeled/tested by Go; the observations show rounding loss and rule out naive balance arithmetic for sequential quotes.

Executed `TickerMonadFork` at Monad block **111021450**. Deployed a fresh escrow, sweeper, timelock, hook implementation, permission-mined CREATE2 proxy, test ERC20, yield source, TickerVault, and test swap router entirely inside Foundry's fork. Used the existing Monad PoolManager and real USDC strategy:

- PoolManager: `0x188d586Ddcf52439676Ca21A244753fA19F9Ea8e`.
- USDC: `0x754704Bc059F8C67012fEd69BC8A327a5aafb603`.
- Strategy: `0xC554aFfE2f581F5E0811e0D42D484ECaC5c6B8e2`.
- Seed: 1,000,000 test tokens and 10,000 USDC; LP fee 0.2%, hook fee 1%, creator tax 0.5%, yield fee 100%.

The pool completed a 100-USDC buy followed by a 1,000-test-token sell. The test verifies strategy shares remain in TickerVault after trading. Both Go quotes match the actual fork's net output, final price and tick exactly. The second Go quote uses the measured post-first-swap snapshot; this does NOT yet prove Go's UpdateBalance or unsettled multi-leg routing.

Reproduce the Solidity tests from this checkout (the supplied Ticker checkout must be at the pinned commit):

```
python3 pkg/liquidity-source/uniswap/v4/hooks/dualpool/testdata/solidity/run-fixtures.py /path/to/ticker-contracts
python3 pkg/liquidity-source/uniswap/v4/hooks/dualpool/testdata/solidity/run-fixtures.py /path/to/ticker-contracts --fork
```

`MONAD_RPC_URL` optionally supplies an archive RPC. The commands never broadcast. The portable runner has syntax validation; the initial runs used an equivalent generated local Foundry configuration.

Targeted Go regression command:

```
CI=true go test ./pkg/liquidity-source/uniswap/v4/hooks/dualpool ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool -count=1
```

Passed. `CI=true` skips upstream's live Robinhood tracker test: its historical RPC state was unavailable when run directly. This does not skip the deterministic DualPool tests or any new Ticker comparisons.

Still incomplete: exact post-teardown state, tracker/registration, reverse quotes, executed shortage/claim route fixtures, broader allocation/rounding cases, router execution encoding, and measured gas. No public branch, fork, PR, or mainnet transaction has been created for this integration.

### Exact teardown and reverse-quote work

The shared opt-in quote now accounts for each position's rounded-up mint cost, rounded-down burn proceeds, and per-step/per-position LP fee growth. All eight mock-strategy Solidity fixtures now compare final balances exactly, in addition to output and price. These balances describe the JIT cycle before any strategy-specific redeposit rounding; modeling an arbitrary ERC-4626 round trip is still outstanding.

Reverse quotes must not binary-search Ticker's net output directly. With two separately floored fees, increasing gross output can increment both fees simultaneously and reduce net output by one. The new reverse helper searches gross output, tests exact fees, and advances if the attainable gross output lands on such a boundary. Exhaustive small-input tests in both directions cover desired outputs around fee boundaries. Full adapter wiring and broader reverse-quote tests remain outstanding.

### Kyber interface wiring (local prototype)

Added the Ticker Hook implementation with BeforeSwap, reverse quotes, CloneState, UpdateBalance, GetReserves, and Track. The tracker reuses the DualPool state reader and separately reads Ticker totalAssets, launches, and vaults, with both requests pinned to the parent tracker's block and overrides. ABI is extracted from the pinned compiled Ticker implementation. The local RPC test exercises ABI decoding and requires both batches to request block 123.

Tests now exercise Kyber's actual v4 PoolSimulator, not only standalone math. The 1001-unit fixture yields 984 units through that interface, and a reverse quote is checked by feeding its input into CalcAmountOut. Clone tests check bucket independence and non-aliasing of quote balances.

A four-swap Solidity sequence also matches Go output, price, and final balances with no intermediate Solidity snapshots injected. This is the mock-strategy fixture, not a proof of arbitrary vault round trips.

Explicit prototype limitations: UpdateBalance currently marks a vaulted pool as requiring refresh, rather than silently using JIT balances as the vault's new claim value. This is temporary and does not satisfy the final routing goal. SwapGas is unset until measured/configured. NewHook is registered only by local tests; there are no invented or automatically registered production addresses. Same-unlock settlement-aware route modeling, vault rounding, real RPC tracker verification on the fresh fork, broader tests, and deployment verification remain required.

### Aave fork accounting model (local, not tracker-enabled yet)

Inspected Aave's pinned `ERC4626StataTokenUpgradeable.sol` at `aave-dao/aave-v3-origin@8305565ae342f1773c42cd2e4593f175fe5968a0`: conversions use `assets * RAY / rate` and `shares * rate / RAY`, with the requested rounding. Added fork telemetry for exact raw cash, claims, pool wrapper shares, wrapper supply/idle cash, StataToken shares, normalized-income rate, pending yield shares, and claim value before/after both trades.

The Ticker storage layout at the pinned commit places Inventory at slot 3: vault mapping slot 3, per-partition shares slot 4, packed raw/claims slot 5. The test reads those slots and verifies that the partition shares equal the hook's wrapper balance in this single-pool fixture. This is test telemetry, not an assumption that global balances represent per-pool inventory.

The new AaveInventory helper models the two share conversions, pending yield dilution, JIT withdrawal shortfall, claims, and redeposit. Both fork swaps match exact wrapper shares, strategy shares, raw cash, claims, supply, rate, and pending yield. A sequential Hook-interface test starts only from the first snapshot, applies the first simulated result, then matches the second fork quote and its inventory without injecting the second snapshot.

This model is not yet automatically populated/enabled by Track. Recognition/code-version checks, complete arithmetic validation, deposit-cap/failure outcomes, liquidity-cap changes, shared strategy state, and settlement-aware claims remain open. The tested sequential case consists of separate fully settled swaps at one timestamp. It must not be described as validation of two swaps inside one unlock.

### Direct local RPC validation

Hardened the Aave arithmetic against missing values, zero rates, inconsistent share ownership, packed-inventory overflow and uint256 addition overflow. Added an exact zero-share-deposit rollback test and checked that failed simulations do not mutate input state.

Added executable-code fingerprints for the pinned hook and wrapper compiler artifacts, masking constructor immutables and excluding metadata. These are accounting-model identification checks, not deployment authorization; immutable configuration still needs validation. Confirmed the recorded Aave implementation hash against its code at Monad block 111021450: `0xaae6d8b1278cc94bb511e0d26bc88c503152c51502178d4e94ccfe041bfedf06`.

A local Anvil instance is running on `http://127.0.0.1:18545`, chain 143, forked at block 111021450 with mining disabled. The new export fixture records setup storage writes (including Aave's seed-deposit changes) and explicitly exports code/storage for 14 accounts. Foundry's dumpState omitted setup storage, so it is not used as evidence. `load-local-rpc.py` refuses non-loopback URLs, non-Anvil clients, and other chains/blocks. The imported state exists only on the local node; no transaction was sent to mainnet.

`TestTrackerAgainstFreshLocalMonadFork` passed against this node. It checks the actual hook/wrapper executable fingerprints, runs the real multicall tracker, verifies both backing amounts and the wrapper binding, and reproduces the Solidity buy output exactly. The initial USDC backing is 9,999,999,999 raw units after the one-unit seed-deposit rounding loss, not the 10,000,000,000 units supplied.

Command actually run:

```
KYBER_LOCAL_RPC=http://127.0.0.1:18545 CI=true go test ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool -run TestTrackerAgainstFreshLocalMonadFork -count=1 -v
```

Local export currently uses `/private/tmp` paths and a generated Foundry config granting that directory access. Make the export runner portable before considering the change PR-ready. Automatic Aave-model loading, all modeled capacity boundaries and unsettled routes are still incomplete.

### Local accounting tracker checkpoint

The full tracker now reads the fresh local Monad/Aave deployment successfully,
including wrapper shares, pending yield, StataToken rate and native capacity
reports. Reads are pinned to the requested block and honor state overrides.
It verifies the pinned implementation fingerprints before using storage layout.
It reads PoolManager cash for **both** ERC-20 currencies and subtracts claims
whose cash has not arrived, including the side without a vault.

Verified with `KYBER_LOCAL_RPC=http://127.0.0.1:18545 CI=true go test
./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool
./pkg/liquidity-source/uniswap/v4/hooks/dualpool -count=1`:

- The tracked fresh deployment produces the recorded first swap output.
- A state override inserts one unbacked claim on the raw-token side; tracking
  catches it and the quote rejects with `ErrVaultIlliquid`.
- Cloned accounting arrays and capacity limits do not share mutable integers.

This remains a prototype: reading Aave limits does not yet model their changes
through withdrawal/redeposit. Capped or paused strategy transitions, same-unlock
route settlement, gas measurement, and final integration wiring remain open.
Native-token pools are explicitly rejected by this accounting tracker until their
balance reads and settlement have equivalent tests. No public changes were made.

### Aave reserve-capacity model checkpoint

Added `AaveReserve` and `AfterJITWithReserve` to model reserve cash, supply-cap
room and active/paused/frozen flags. Withdrawal conversion preserves both floors
(cash -> shares -> cash). A withdrawal frees supply-cap room but does not lift a
freeze. Failed redeposits retain raw pool inventory. Quoting clones reserve state.
The arithmetic references Aave v3.5 TokenMath and StataToken source pinned at
`8305565ae342f1773c42cd2e4593f175fe5968a0`.

Targeted Go tests pass for cash rounding, cap recovery, frozen redeposits and
paused withdrawals. The fresh Monad fork test
`test_depositDisabledKeepsRawCashAndStillTrades` also passes: mocking only the
strategy's `maxDeposit` result to zero leaves raw USDC after the first swap, and
the reverse swap succeeds. This verifies Ticker's deposit-failure catch against
real deployed strategy state; it is not an Aave governance freeze or a proof of
all supply-cap transitions.

Outstanding: `AaveReserve` is not yet populated by the tracker or consumed by
`Hook.BeforeSwap`. Reserve state must include treasury accrual at the quote
 timestamp, and deployed reserve implementation behavior must be verified before
connecting it. Existing tracked `AaveLimits` alone are insufficient to predict
post-withdrawal capacity. The fixture runner now accepts `--match-test` for
reproducing individual fork regressions.

### Reserve tracker wiring (supersedes the previous unwired checkpoint)

The tracker now reads reserve configuration, virtual underlying cash, scaled
supply/debt and normalized debt index at the same pinned block. It computes the
pending treasury accrual separately from the stored amount: native `maxDeposit`
uses stored treasury, while a subsequent pool mutation accrues interest before
validating the supply cap. Both modeled native limits must exactly match the
strategy's RPC results before the snapshot is accepted.

`Hook.BeforeSwap` now uses this reserve state for withdrawal/redeposit simulation,
and recalculates available vault cash before applying the full-liquidity check.
`UpdateBalance` and `CloneState` carry independent copies of reserve state.

The local RPC regression now starts with a full tracked snapshot, quotes a buy,
applies its simulated changes, and quotes the reverse sale. Both outputs match
the recorded fork fixture without injecting an intermediate snapshot. Simulated
zero cash and paused reserves reject quotes; frozen deposits preserve raw cash
and permit the reverse trade. A unit regression covers treasury accrual making
a deposit fail despite a positive pre-update `maxDeposit` report.

This does not complete route settlement, shared reserves across different pools,
gas estimation, or deployed reserve implementation verification. The captured
fork scenario has a recently updated reserve; an elapsed-time fork case should
also compare treasury accrual and cap transitions against execution.

### Payment timing and claims

The simulator now updates physical PoolManager cash and per-pool raw cash/claims,
including the side without a vault. Redeeming old claims removes physical cash
from PoolManager; a negative JIT delta pays cash back; a positive JIT delta only
mints claims. For exact-input swaps Ticker takes fee tokens immediately, then router
settlement removes net user output. Together they remove gross output. Exact-output
fee claims are a different contract path; Kyber executes exact-input even when
requesting a reverse quote. New claims are unbacked only to the extent they exceed
PoolManager's remaining physical cash.

Normal `UpdateBalance` models a settled router leg. The local execution harness
can instead call `UpdateBalanceWithoutSettlement`: its next quote then applies
the same 100% check using unpaid claims. The fresh fork's tracked state passes
the regression where a test-token sale followed by another sale is rejected
without settlement and accepted after settlement. Unit tests cover the cash and
claim movements and partial backing from other PoolManager cash.

**Production routing is not verified:** inspected the public
[ks-dex-adapter-lib tree](https://github.com/KyberNetwork/ks-dex-adapter-lib/tree/afa5c6c29389918fd32e0e28817865d8da2cdf98)
and it contains no Uniswap V4 adapter. Before enabling a route that reuses this
pool, verify the actual executor's payment timing from source or a deployed trace.
The harness method does not automatically teach Kyber's route builder that timing.

Contract confirmation: running
`forge test --match-test test_feeActiveDoubleSwapSingleUnlockSecondLegRefusedAllModes`
in the fixture project passed all three inherited suites (Ticker fixture, base
fee suite and LP-fee suite). Each tests refusal of the second unpaid leg in both
directions and exact-input/exact-output modes, including rollback assertions.
This confirms the contract's deferred-settlement behavior; it does not identify
Kyber's production executor's settlement policy.

### Gas and quote validation checkpoint

Measured complete `PoolSwapTest.swap` calls through local Anvil's
`eth_estimateGas`, using the exported fresh Monad/Aave fixture. No transaction
was broadcast. Four independent initial-state quotes (both directions and two
sizes per direction) measured:

- One range: 1,376,968–1,399,609 gas.
- Eight ranges: 2,428,318–2,439,949 gas.

`testdata/gas.json` records the inputs and results. `measure-local-gas.py`
reproduces estimates against loopback Anvil; export with
`KYBER_FIXTURE_BUCKETS=8` to measure eight ranges. The node was restored to the
one-range fixture afterward.

The hook now returns a nonzero estimate: 1,450,000 + 175,000 per additional range
+ 25,000 per crossed tick word, with an extra 500,000 allowance for a second Aave
vault. Kyber's parent v4 simulator adds its own base gas. Tests check the one- and
eight-range estimates cover the measured fixtures. This is a routing heuristic,
not a worst-case bound. Second-vault cost, actual Kyber executor overhead, and
Monad execution/billing require deployment-stage verification.

The new exact quote entry point also rejects more than eight ranges, zero
weights, out-of-range ticks, invalid directional protocol fees, and liquidity
exceeding PoolManager's per-tick limit. Mint/burn accumulation checks overflow.
Existing legacy DualPool entry points retain their prior behavior.

### Physical cash correction and regression audit

Review of `_afterSwap` found and corrected an error in the newly added cash
model: exact-input fees are taken as tokens, not minted as claims. The simulator
now subtracts fees before exposing the state between swaps, then subtracts net
output at router settlement. `testdata/manager-cash.json` records actual
PoolManager balances before and after both fork swaps; the tracker/quote/update
regression matches both currencies exactly after each swap. This stronger check
would have failed with the earlier fee treatment even though output quotes passed.

Tracker recognition now also checks the compiled ERC1967Proxy runtime and reads
`poolManager()` to verify the expected Monad address. These checks complement the
implementation fingerprint rather than trusting a storage slot on arbitrary code.

`CI=true go test ./pkg/liquidity-source/uniswap/v4/... -count=1` passed across the
V4 integration and existing hook packages. The targeted local RPC regression also
passed with explicit physical-cash assertions. Deployment/executor verification,
shared-reserve route handling, final documentation consolidation and the remaining
fixture/configuration coverage are still required before declaring completion.
