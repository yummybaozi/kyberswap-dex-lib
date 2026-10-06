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
