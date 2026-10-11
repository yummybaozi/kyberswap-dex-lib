# Ticker DualPool on Monad

TickerDualPool uses temporary Uniswap V4 liquidity backed by vault assets. This
integration reuses the existing V4 factories and DualPool allocation/swap math.
Its exchange identifier is `uniswap-v4-ticker-dualpool`; the parent pool type
remains `uniswap-v4`. Production hook registration remains disabled.

## Pricing and state

TickerDualPool calls TickerVault, which withdraws from and redeposits into the
supported Aave strategy. The tracker reads distribution, price, launch terms,
backing and available cash at one block and checks reviewed executable fingerprints.
The simulator preserves bitmap-word stepping, mint/burn/LP-fee rounding, vault
share rounding, deposit caps, treasury accrual and frozen/paused states.
Shares worth 1,000 USDC with only 400 USDC withdrawable cannot back a valid quote.

Hook fees and creator tax are separately rounded down on gross output:
`net = gross - floor(gross * hookFeeBps / 10000) - floor(gross * creatorTaxBps / 10000)`.
Two 1% charges on 150 units deduct 1 + 1. `CalcAmountIn` reverses exact-input
execution; it does not simulate the contract's on-chain exact-out mode.

Quotes are pure; `UpdateBalance` consumes saved quote state and clones own their
mutable data. The optional V4 shared-inventory interface passes `SwapLimit` so
multiple pools see the same Aave cash, vault supply and PoolManager balances.
Snapshots use the existing V4 `HookExtra` convention and preserve their block.
The hook and interface-embedded `BaseHook` are registered for msgpack, including
an existing Twofold simulator regression.

## Contracts and activation

Monad chain ID: 143. Fork block: **112288351**.

| Contract | Address |
| --- | --- |
| DualPool proxy | `0x047ea2f55148b7601fdde0d28588b55c09192ac4` |
| Implementation | `0x844a14bae1b67df6064bb87ba5ff941dc02649e0` |
| PoolManager | `0x188d586ddcf52439676ca21a244753fa19f9ea8e` |
| Kyber router | `0x6131b5fae19ea4f9d964eac0408e4408b66337b5` |

Execution coverage uses ERC-20 pairs and Aave USDC. Morpho, arbitrary strategies
and native-token execution are outside this coverage. USDT0/WETH recognition
entries do not establish equivalent execution coverage. Activation still needs
address registration, production discovery/tracking, route-builder shared cash
and payout handling, Kyber service signing and gas calibration. The existing
DualPool gas heuristic is preserved; Forge gas is not a production Monad estimate.

## Reproduce

```sh
CI=true go test ./pkg/liquidity-source/uniswap/v4/... ./pkg/msgpack ./pkg/pooltypes ./pkg/test -count=1
CI=true go test -race ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool ./pkg/liquidity-source/uniswap/v4/hooks/dualpool -count=1
go generate ./pkg/msgpack/...
```

Tests cover captured quotes, rounding, dust, range boundaries, Aave limits,
sequential state, shared cash, stale snapshots, clones and serialization.
Captured fixture values live directly in the Go test files. `CI=true` skips
upstream remote tests.

Solidity/Python tooling and fork-only Go bridges live in the private
[contracts harness](https://github.com/jaybutera/ponsfamily/tree/e068291086e8e32dfcba29268536f00b124ca8ef).
With repository access, Foundry, an archive RPC and initialized submodules:

```sh
git checkout e068291086e8e32dfcba29268536f00b124ca8ef
python3 tools/kyber/run-mainnet-go.py --kyber-checkout /absolute/path/to/this-checkout --venue ticker-dualpool --fork-block 112288351 --report-file deployments/kyber-dualpool-split-run.json
```

The [run report](https://github.com/jaybutera/ponsfamily/blob/codex/kyber-mainnet-tests/deployments/kyber-dualpool-split-run.json)
pins Go/harness commits, bridge/runtime hashes, commands and quotes. Six cases
cover buy/sell, a round trip, a large trade, repeated trades, shared Aave cash,
and shortage/rollback/recovery. Go quotes precede deployed Kyber execution.
Completion, launches, funding and signer authorization stay inside the fork.
Fixture provenance remains in the
[original evidence branch](https://github.com/yummybaozi/kyberswap-dex-lib/tree/39a2df390ed33dd8e8c56fc80072ec7961b8a005).
