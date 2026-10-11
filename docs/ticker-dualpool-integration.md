# Ticker DualPool — local Kyber integration

**Status:** review branch `codex/ticker-dualpool-submission` in
[yummybaozi/kyberswap-dex-lib](https://github.com/yummybaozi/kyberswap-dex-lib).
Based on upstream `main` commit `e3b08f15cd94d9bfafc393c9014e0d1cb787e011`.
No upstream PR is opened by this work. No production hook address is registered;
live activation still requires the checks below.

See the [combined test evidence and Kyber submission review](ticker-kyber-validation.md)
for the current twelve-case report, reproduction commands and remaining review gates.

The earlier pinned sources and measurements below remain historical evidence.
The deployed-foundation validation section at the end records the current branch's
execution checks.

## Pinned sources

- [Ticker source](https://github.com/jaybutera/ponsfamily/tree/a8b41e44916d3d6266f8d38f2f3cd116c2379207): `a8b41e44916d3d6266f8d38f2f3cd116c2379207`.
- [Kyber baseline](https://github.com/KyberNetwork/kyberswap-dex-lib/tree/8a954853c26c8e7a799f810c1ec05e2da0165954): `8a954853c26c8e7a799f810c1ec05e2da0165954`.
- [Aave StataToken and reserve arithmetic](https://github.com/aave-dao/aave-v3-origin/tree/8305565ae342f1773c42cd2e4593f175fe5968a0/src/contracts): `8305565ae342f1773c42cd2e4593f175fe5968a0`.
- Monad fork source block: **111021450**; chain ID **143**.

## What changes

Kyber already models DualPool's temporary liquidity positions. This integration reuses its allocation and swap helpers, adds accurate bitmap-word stepping for Ticker, and puts Ticker's fees, full-liquidity check, and Aave accounting in `hooks/ticker-dualpool`.

The existing DualPool entry point keeps its original stepping path. New quote validation and the spacing-aware entry point are opt-in. Parent V4 changes are optional interfaces for shared inventory and exact post-swap state; existing hooks retain their behavior unless they implement those interfaces.

### Quote flow

1. The parent tracker selects a block. Every Ticker RPC read and state override uses that block.
2. The tracker reads price, ranges, fees, total backing, available inventory, raw tokens, claims and wrapper shares. It verifies the expected proxy and implementation code and the hook's PoolManager before reading storage layout.
3. For recognized Aave strategies, it reads the wrapper and reserve state. Calculated native deposit/withdrawal limits must match the strategy's reported limits exactly.
4. The quote subtracts claims whose physical cash is missing. If either currency cannot supply its **entire backing**, it returns `ErrVaultIlliquid`. Shares worth 1,000 USDC with only 400 USDC available do not produce a tradable 400-USDC quote.
5. The shared DualPool math allocates ranges and reproduces v4's steps, mint/burn rounding and LP fee growth. It stops at bitmap-word boundaries, including empty ones. Identical ranges share a position because DualPool uses the same `LP_SALT`, so their fees are collected with one rounding.
6. Ticker charges separately rounded output fees:

   ```text
   net output = gross output
              - floor(gross output × hookFeeBps / 10000)
              - floor(gross output × creatorTaxBps / 10000)
   ```

7. The simulator models withdrawal, JIT teardown, redeposit or retention as raw cash, then router settlement. `UpdateBalance` consumes that result; it does not rerun pricing. Clones have independent mutable state.

Simply composing the existing DualPool BeforeSwap handler with the classic Pons AfterSwap fee handler does not work: that handler can see zero ordinary output after BeforeSwap deltas. Ticker applies fees to the gross output calculated by DualPool.

Reverse quotes still execute exact-input swaps. Net output is not strictly monotone when both independently rounded fees increment together, so reverse search uses gross output and checks the exact fee result. It checks backing before searching, preserving shortage errors.

### Aave and shared state

`AaveInventory` models this pool's wrapper shares, wrapper supply, idle cash, strategy shares and pending yield dilution. `AaveReserve` models reserve cash, supply cap, active/paused/frozen flags and treasury accrual. Cash-to-shares-to-cash preserves both floors. Failed deposits leave raw pool cash, as Ticker's caught-deposit path does.

Treasury interest is stored separately until the next reserve mutation. This matters because native `maxDeposit` reads stored treasury, while Aave's subsequent validation sees freshly accrued treasury. A positive reported deposit limit can therefore still lead to a caught deposit failure.

Pools can share Aave cash, a wrapper, or PoolManager cash. Optional `HookWithSwapLimit` wiring passes Kyber's route inventory through forward/reverse quotes and updates. Shared keys hold those common totals; pool-owned shares, raw tokens and claims remain local. Quotes overlay shared values onto a clone. Applying a result rejects stale prestates and mixed snapshot blocks. The parent simulator receives the hook's exact backing, price and zero post-JIT liquidity.

Route construction must supply this inventory to model interactions between different simulators. The ordinary nil-limit path only models the local pool's sequence.

### Settlement timing

For exact-input swaps, Ticker **takes fee tokens immediately**. The router later pays input and takes net output. Together the output fee take and router take remove gross output from PoolManager.

Normal `UpdateBalance` models a settled leg. The execution-aware local harness can use `UpdateBalanceWithoutSettlement` to test another swap before payment. It subtracts unbacked claims using actual PoolManager cash; existing cash may already cover some claims.

**Kyber service-generated route timing remains unverified.** The inspected [public adapter repository](https://github.com/KyberNetwork/ks-dex-adapter-lib/tree/afa5c6c29389918fd32e0e28817865d8da2cdf98) does not contain its Uniswap V4 adapter. The harness method does not automatically configure Kyber's production route builder. Verify the executor before enabling routes that reuse a pool without intermediate settlement.

## Local deployment and supported model

The fork deploys a fresh escrow, sweeper, timelock, implementation, permission-mined ERC1967 proxy, test token, yield recipient, TickerVault and test router. It uses these existing Monad contracts:

| Contract | Address |
| --- | --- |
| PoolManager | `0x188d586Ddcf52439676Ca21A244753fA19F9Ea8e` |
| USDC | `0x754704Bc059F8C67012fEd69BC8A327a5aafb603` |
| Aave USDC StataToken | `0xC554aFfE2f581F5E0811e0D42D484ECaC5c6B8e2` |
| Aave Pool | `0x69a5F9AD4f96ebf0a0C792dD42a01cC5C0102fef` |

Seed: 1,000,000 test tokens and 10,000 USDC; 0.2% LP fee, 1% hook fee, 0.5% creator tax, 100% yield fee. Strategy conversion initially values the deposit one USDC base unit below 10,000 USDC; fixtures retain that rounding.

The tracker recognizes the pinned Ticker implementation and configured Monad StataToken code fingerprints. USDC has full local fork execution coverage. Configured USDT0/WETH proxies have recognition entries but do not yet have equivalent execution fixtures. Arbitrary ERC-4626 strategies, Morpho, native-token pools, upgraded/unknown implementations and nonmatching capacity reports are rejected by this tracker. This is not a generic ERC-4626 simulation model.

## Evidence

| Check | Evidence and scope |
| --- | --- |
| Exact fees and JIT accounting | Eight mock-strategy Solidity cases match output, separate fees, price, tick and final backing. |
| Multiple ranges | 18 Solidity cases cover eight overlapping ranges, separated ranges with gaps and identical ranges; both directions and three sizes. All compared integer values match. |
| Sequential trades | Four mock-strategy swaps match without intermediate snapshots. Fresh Aave buy/sell matches through tracker → quote → update → reverse quote, including wrapper inventory and physical PoolManager cash. |
| 100% floor | Go tests reject partial/zero capacity and unpaid claims. Solidity's deferred-settlement regression passes across three inherited suites, including rollback checks. |
| Shared reserve | Parent-simulator tests cover forward/reverse quotes and clone isolation. A two-pool Monad fork test reduces Aave's virtual cash with a local storage edit; pool A's trade makes pool B revert `VaultIlliquid`; restored cash permits trading. |
| Deposit failure | A fork test mocks only `maxDeposit = 0`; raw cash remains after the swap and reverse trading succeeds. Go tests cover frozen deposits, paused withdrawals and supply-cap recovery. |
| Directional protocol fees | Four Monad-fork swaps with different protocol fees in each direction match net output, both output fees, final price and tick exactly. |
| Elapsed interest | After a one-day fork time advance, calculated treasury shares, post-withdrawal reserve supply and cash exactly match Aave execution. |
| Fuzzing | Conservation/purity: 627,038 executions in 20 seconds. Reverse known-output search: 70,084 in 20 seconds. Short reruns after the duplicate-position fix also pass. |
| Serialization | Msgpack registration regenerated; a serialized/restored V4 simulator preserves Ticker/Aave quotes through two updates. |
| Compatibility | Final full V4 Go test tree, msgpack tests and Ticker/DualPool race checks passed. `CI=true` skips existing remote live tests; the explicit local RPC regression remains enabled via its environment variable. |

Gas estimates from complete local test-router calls: **1.38–1.40M** with one range, **2.43–2.44M** with eight ranges. The hook estimate is 1,450,000 + 175,000 per additional range + 25,000 per crossed tick word, plus an unmeasured 500,000 allowance for a second Aave wrapper. The parent adds base gas. This is a routing heuristic, not a worst-case bound or a Monad mainnet fee measurement. Inputs/results are in `testdata/gas.json`.

## Reproduce

Prerequisites: Go, Foundry, Python 3, and a Ticker checkout at the pinned commit with dependencies installed. The runner rejects tracked changes to Ticker source/tests. `MONAD_RPC_URL` optionally specifies an archive RPC. All deployments and storage edits are local; no script broadcasts to mainnet.

```sh
CI=true go test ./pkg/liquidity-source/uniswap/v4/... ./pkg/msgpack -count=1

python3 pkg/liquidity-source/uniswap/v4/hooks/dualpool/testdata/solidity/run-fixtures.py /path/to/ticker-contracts
python3 pkg/liquidity-source/uniswap/v4/hooks/dualpool/testdata/solidity/run-fixtures.py /path/to/ticker-contracts --fork
```

Use `--match-test` to select the range, shared-reserve, deposit-failure or elapsed-interest tests. `extract-range-fixtures.py` validates a successful Forge log and regenerates the 18 range fixtures.

For real RPC tracking of the fresh local deployment, start:

```sh
anvil --host 127.0.0.1 --port 18545 --fork-url https://rpc.monad.xyz --fork-block-number 111021450 --code-size-limit 131072 --no-mining
```

Then:

```sh
python3 pkg/liquidity-source/uniswap/v4/hooks/dualpool/testdata/solidity/run-fixtures.py /path/to/ticker-contracts --fork --export-rpc --output-dir /tmp/ticker-kyber-portable
python3 pkg/liquidity-source/uniswap/v4/hooks/dualpool/testdata/solidity/load-local-rpc.py --allocs /tmp/ticker-kyber-portable/ticker-kyber-rpc-allocs.json
KYBER_FIXTURE_MANIFEST=/tmp/ticker-kyber-portable/ticker-kyber-rpc-manifest.json KYBER_LOCAL_RPC=http://127.0.0.1:18545 CI=true go test ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool -run TestTrackerAgainstFreshLocalMonadFork -count=1
```

The portable export → load → tracker sequence has passed. The loader refuses non-loopback nodes, non-Anvil clients and an incorrect chain/source block. `testdata` directories contain nested `go.mod` files so fixture sources stay out of downstream module downloads.

## Remaining review and deployment gates

- Review untested strategy configurations before claiming their execution support.
- Confirm the deployed Aave core/strategy versions and contract settings against the modeled assumptions at the deployment block. Unknown upgrades require review.
- After an authorized Ticker deployment, register its verified hook address through `RegisterHooksFactory(NewHook, address)` and provide initialized pool keys to the existing V4 discovery/tracking pipeline. No address is invented or automatically enabled here.
- Verify Kyber's actual transaction encoding, hook data, settlement timing and use of shared route inventory. Execute both directions and reverse-requested quotes against that executor.
- Measure actual executor gas and Monad billing; verify extra configurations, especially two wrapped sides, before treating their gas allowance as measured.
- The deployed-foundation fixture described below retains the live hooks but creates the rest of its test state locally. No mainnet deployment or transaction, upstream PR, or production routing activation is part of this branch.

## Go quotes consumed by the real Kyber executor

The additional `TestExecutorBridge` is opt-in. The Ticker-side runner exports a fresh
Monad-fork pool snapshot, starts a loopback Anvil, then supplies `KYBER_BRIDGE_REQUEST`,
`KYBER_BRIDGE_RESPONSE` and `KYBER_LOCAL_RPC`. The test runs the real tracker and V4
simulator and returns output, fees and predicted reserves/price before any swap executes.
It also quotes repeated-pool and shared-Aave routes using `UpdateBalance` and shared
inventory. Ordinary Go tests skip this bridge; they do not prove executor compatibility.

The companion Ticker harness is on `codex/kyber-quote-execution` in ponsfamily; see its
`docs/kyber-quote-execution.md` for exact commands, assertions and limitations. A local
signer is authorized only on the fork. It does not obtain Kyber production signatures.

Fresh staged deployments exposed an overly strict proxy check: compiler metadata changed
while proxy instructions did not. The tracker now fingerprints the exact executable
ERC-1967 proxy instructions, while retaining implementation, PoolManager, strategy and
accounting validation. Regression tests accept changed metadata and reject changed code.


## Deployed Monad foundation validation

The review branch carries the complete updated internal #152 implementation,
rebased onto upstream main `e3b08f15cd94d9bfafc393c9014e0d1cb787e011`.
The production Go changes are in their actual Kyber paths; this branch does not
include the contracts repository's patch/source review-bundle directory.

The exact reviewed deployed implementation fingerprint is accepted in addition
to the earlier fingerprint. Three transient SWEEPING_SLOT constants and one
buyback event topic account for the normalized executable difference. A runtime
fixture regression recovers the older fingerprint by replacing those constants,
accepts both reviewed versions, and rejects mutated executable instructions.
The deployed-code update does not change pricing calculations or storage layout.

The fork fixture retains the following deployed contracts at block 112288351:

| Contract | Address |
| --- | --- |
| Ticker DualPool hook proxy | `0x047ea2f55148b7601FDDe0d28588B55c09192ac4` |
| Ticker DualPool implementation | `0x844A14BaE1B67DF6064bB87BA5Ff941DC02649e0` |
| PoolManager | `0x188d586Ddcf52439676Ca21A244753fA19F9Ea8e` |

The remaining stack is completed and tokens are launched only in the fork.
Hook proxy addresses, implementation pointers and bytecode are checked unchanged.
A test signer is authorized only in local fork storage. No transaction is sent to
mainnet, and the fork-created token/pool addresses are not production registrations.

The six scenarios cover buy/sell, a round trip in one transaction, a 5,000-USDC
buy and full sale, repeated 1/100/1,000-USDC buys and partial sells, two pools
sharing Aave USDC cash, and shortage/rollback/recovery. Successful execution must
match Go output, complete input, fees, creator tax, reserves and price exactly.
The shortage is deliberately created locally: available Aave cash drops from
10,001 USDC to 9,996.112190 USDC after the first simulated leg. Go rejects the
second pool; the attempted two-hop route must roll back both pools and trader
balances. Restoring cash must allow the route to succeed.

The [contracts harness](https://github.com/jaybutera/ponsfamily/tree/42a7286301bedf7558f036b87539a2caa0dd6aac)
contains the state exporter, loopback Anvil bridge and executor runtime pins.
From that checkout, with Foundry dependencies installed, reproduce using:

```sh
python3 tools/kyber/run-mainnet-go.py \
  --kyber-checkout /absolute/path/to/this-kyber-checkout \
  --fork-block 112288351
```

Run serially; the harness uses shared scratch files. An archive RPC can be supplied
with `--fork-url`. The runner opts into FFI and ordinary Go tests skip the bridge.

Our route encoder clears step amount bit 255 to use the current route's token
credits; selecting balance-minus-one mode underpays from an initially empty
executor. This encoder change belongs to the contracts harness, not the Go quote
math. Kyber's production route builder must use equivalent handling and supply
shared inventory for routes crossing pools backed by the same Aave reserve.
Production service-generated routes, hook activation, finalized execution gas,
Morpho/arbitrary strategies and the meme-hook Go model remain separate work.


On October 10, 2026, all six deployed-foundation scenarios passed on this rebased
Go implementation at commit `9a80312271f23f2188878d3a475c17ae5df3d2cb`:
6 passed, 0 failed, 0 skipped. Full V4/msgpack tests and Ticker/DualPool race checks
also passed. Formatting and serialization regeneration produced no source changes.
[Exact quote and validation evidence](../pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool/testdata/mainnet-executor.json)
is included under the nested testdata module, excluded from downstream downloads.
Subsequent changes add only this documentation and evidence, not Go code.

## Companion meme-hook model

The separate [TickerMemeHook integration](ticker-memehook-integration.md) adds
classic V4 pool output-fee simulation. It preserves this DualPool model's
accounting, Aave shared inventory and gas logic. Both production hook addresses
remain unregistered. The companion foundation runner now tests both venues.

## Current deployed-hook checkpoint

Both hook models passed through the deployed Kyber router/executor at Monad
block **112288351**: **12 passed, 0 failed, 0 skipped**, six per venue.
Tested Go code: `82fba4445d0169a1370d1cf276a024a2f353b406`; tested harness code:
`eb8284b48e8c5b3f8737d8f0029eb6203f96e97f`. The companion
[PR #199 evidence checkpoint](https://github.com/jaybutera/ponsfamily/tree/4e1c2c0c107ef8793d1071902bb47503f5eae634)
includes the report; later documentation/evidence commits do not change tested code.

[Complete executor evidence](../pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook/testdata/mainnet-executor.json)
and [Go check results](../pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook/testdata/validation.json)
record exact commands, source provenance, runtime pins and quote values.
Meme execution covers buy/sell with tax 0 and 133 bps, repeated trades,
simulated round trips in one transaction, reverse quotes in both directions,
and one-unit overstated quote rollback. Input, output, separately rounded fees,
price, tick and liquidity match exactly. Reverse quotes guarantee at least the
request, not minimum input. DualPool pricing, Aave inventory and gas code are
unchanged. Production address registration remains disabled.
