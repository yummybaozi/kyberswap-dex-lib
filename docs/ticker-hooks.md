# Ticker hooks on Monad

Ticker has two graduation venues: DualPool uses temporary Uniswap V4 liquidity
backed by vault assets; classic launches use TickerMemeHook on ordinary V4 pools.
Both reuse the existing `uniswap-v4` discovery, tracker and simulator factories.
Their exchange identifiers are `uniswap-v4-ticker-dualpool` and
`uniswap-v4-ticker-memehook`. Production hook addresses remain unregistered.

## Pricing and state

DualPool reads its distribution, price, launch terms, backing and available cash
at one block. It reuses the existing DualPool allocation/swap math, with exact
bitmap-word stepping and mint/burn/LP-fee rounding. TickerDualPool calls
TickerVault, which withdraws from and redeposits into the supported Aave strategy.
The model preserves share rounding, deposit caps, treasury accrual and frozen or
paused states. It refuses a swap when the full backing cannot be withdrawn:
shares worth 1,000 USDC with only 400 USDC available do not yield a tradable quote.

Both hooks charge fees on gross output, rounding each charge down separately:

```text
output = gross - floor(gross * hookFeeBps / 10000)
               - floor(gross * creatorTaxBps / 10000)
```

For example, two 1% charges on 150 units deduct 1 + 1 and leave 148 units.
Meme pools otherwise use the parent V4 curve and liquidity state. Their frozen
launch terms use V4's existing `Extra.HookExtra` schema and are cached once the
pool is registered; no new standalone pool type or storage schema is introduced.

`CalcAmountIn` reverses exact-input execution and returns the smallest input
that reaches the requested output. Separately rounded fees can make net output
fall by one unit at a fee boundary, so the search uses monotone gross output and
checks the exact fees. The meme hook returns fee, gas and `SwapInfo` from a forward
quote at that input. This does not model the contract's on-chain exact-out mode.

Quotes are pure; `UpdateBalance` consumes their saved state and clones own their
mutable data. Optional V4 hook interfaces pass shared route inventory and allow
the meme hook's reverse search. For DualPool, the route must supply `SwapLimit`
so multiple pools see the same Aave cash, vault supply and PoolManager balances.
Both concrete hook types and the interface-embedded `BaseHook` are registered
for msgpack; regressions cover the existing Twofold model as well.

## Deployed contracts and supported scope

Monad chain ID is 143. The executor fixture retains these contracts at block
112288351; all test launches and signer authorization occur only on a local fork.

| Contract | Address |
| --- | --- |
| DualPool proxy | `0x047ea2f55148b7601fdde0d28588b55c09192ac4` |
| DualPool implementation | `0x844a14bae1b67df6064bb87ba5ff941dc02649e0` |
| Meme hook proxy | `0x223f8b1b43aeec86d2659cce36169f72f7fc2044` |
| Meme implementation | `0x2d96e4e89d37551ff55eb9cfead78da1278d5986` |
| PoolManager | `0x188d586ddcf52439676ca21a244753fa19f9ea8e` |
| Kyber router | `0x6131b5fae19ea4f9d964eac0408e4408b66337b5` |

The DualPool tracker verifies reviewed executable fingerprints before reading
storage. Execution coverage uses ERC-20 pairs with Aave USDC; Morpho, arbitrary
strategies and native-token execution are outside this submission's coverage.
USDT0/WETH strategy recognition entries do not establish equivalent execution
coverage. The meme fixture constructs a full-range V4 pool from a pinned snapshot;
the full production discovery/tracker pipeline remains an activation check.

DualPool retains its existing local gas heuristic. Meme uses the parent V4 gas
accounting. Forge measurements are not production Monad gas estimates. Address
registration, final gas calibration, production route building/shared inventory
and Kyber service signing remain activation requirements.

## Reproduce the Go checks

```sh
CI=true go test ./pkg/liquidity-source/uniswap/v4/... ./pkg/msgpack ./pkg/pooltypes ./pkg/test -count=1
CI=true go test -race ./pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool ./pkg/liquidity-source/uniswap/v4/hooks/dualpool -count=1
go generate ./pkg/msgpack/...
```

The Go tests cover exact outputs, separately rounded fees, dust, range boundaries,
Aave limits, repeated trades, shared cash, stale snapshots, clone independence,
serialization and minimum-input reverse quotes. Small fixtures are read from
disk; nested `testdata/go.mod` files exclude them from downstream module downloads.
`CI=true` skips upstream remote tests; these commands do not run the fork harness.

## External executor evidence

The [contracts harness](https://github.com/jaybutera/ponsfamily/tree/5183f0d1b96f641a24e7c24ee13e68a8d95498a9)
owns the Solidity/Python tooling and fork-only Go bridges. It requires access to
that private repository, Foundry and an archive RPC. It injects its Go bridge
tests through a file overlay and leaves this checkout unchanged.

From the harness checkout, initialized with its Git submodules:

```sh
git checkout 5183f0d1b96f641a24e7c24ee13e68a8d95498a9
python3 tools/kyber/run-mainnet-go.py --kyber-checkout /absolute/path/to/this-checkout --fork-block 112288351 --report-file deployments/kyber-focused-go-run.json
```

The [execution report](https://github.com/jaybutera/ponsfamily/blob/codex/kyber-mainnet-tests/deployments/kyber-focused-go-run.json)
records the tested Go/harness commits, bridge hashes, deployed runtime hashes,
commands and results. Twelve cases cover both directions, creator tax 0/133 bps,
repeated trades, round trips, shared Aave cash/rollback/recovery, reverse quotes
and an overstated quote that reverts. Go predicts the results before Kyber executes.

Fixture provenance is retained in the earlier
[full evidence branch](https://github.com/yummybaozi/kyberswap-dex-lib/tree/39a2df390ed33dd8e8c56fc80072ec7961b8a005).
The trimmed meme fixtures preserve captured values; historical reverse inputs
are replayed as exact-input executions and are not labeled minimum inputs.
