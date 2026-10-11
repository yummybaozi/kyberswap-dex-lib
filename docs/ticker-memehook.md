# TickerMemeHook on Monad

Classic Ticker launches graduate to ordinary Uniswap V4 pools with TickerMemeHook.
This integration reuses the V4 factories, pricing curve and liquidity state. Its
exchange identifier is `uniswap-v4-ticker-memehook`; the parent pool type remains
`uniswap-v4`. Production hook registration remains disabled.

## Pricing and state

The tracker reads the frozen launch terms from `launches(poolId)` at the refresh
block and stores them in V4's existing `Extra.HookExtra` schema. Registered terms
are cached. Hook fees and creator tax are separately rounded down on gross output:
`net = gross - floor(gross * hookFeeBps / 10000) - floor(gross * creatorTaxBps / 10000)`.
Two 1% charges on 150 units deduct 1 + 1, leaving 148 units.

`CalcAmountIn` returns the smallest input whose exact-input execution reaches
the desired output. Separately rounded fees can make net output fall by one
unit at a fee boundary. The optional V4 reverse-quote interface therefore lets
the hook search monotone gross output and check exact fees. Fee, gas and saved
post-swap state come from a forward quote at the selected input. This does not
simulate the contract's on-chain exact-out mode.

Quotes are pure; the parent V4 simulator applies saved state and clones its
mutable pool data. The hook's terms do not change during a swap. Both Hook and
the interface-embedded `BaseHook` are registered for msgpack; an existing Twofold
simulator regression also covers the shared serialization fix.

## Contracts and activation

Monad chain ID: 143. Fork block: **112288351**.

| Contract | Address |
| --- | --- |
| Meme hook proxy | `0x223f8b1b43aeec86d2659cce36169f72f7fc2044` |
| Implementation | `0x2d96e4e89d37551ff55eb9cfead78da1278d5986` |
| PoolManager | `0x188d586ddcf52439676ca21a244753fa19f9ea8e` |
| Kyber router | `0x6131b5fae19ea4f9d964eac0408e4408b66337b5` |

The executor fixture constructs a full-range ERC-20 pool from a pinned snapshot.
The full production discovery/tracker pipeline, address registration, route
building, Kyber service signing and gas calibration remain activation checks.
Native-token execution is outside the fork coverage. Meme uses parent V4 gas
accounting; Forge measurements are not production Monad gas estimates.

## Reproduce

```sh
CI=true go test ./pkg/liquidity-source/uniswap/v4/... ./pkg/msgpack ./pkg/pooltypes ./pkg/test -count=1
CI=true go test -race ./pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook -count=1
go generate ./pkg/msgpack/...
```

Tests cover fixture replay, fee rounding/dust, exhaustive small-input reverse
quotes, fee boundaries, sequential state, clones and serialization. Fixtures
are read from disk and excluded from downstream module downloads by
`testdata/go.mod`. `CI=true` skips upstream remote tests.

Solidity/Python tooling and fork-only Go bridges live in the private
[contracts harness](https://github.com/jaybutera/ponsfamily/tree/e068291086e8e32dfcba29268536f00b124ca8ef).
With repository access, Foundry, an archive RPC and initialized submodules:

```sh
git checkout e068291086e8e32dfcba29268536f00b124ca8ef
python3 tools/kyber/run-mainnet-go.py --kyber-checkout /absolute/path/to/this-checkout --venue ticker-memehook --fork-block 112288351 --report-file deployments/kyber-memehook-split-run.json
```

The [run report](https://github.com/jaybutera/ponsfamily/blob/codex/kyber-mainnet-tests/deployments/kyber-memehook-split-run.json)
pins Go/harness commits, bridge/runtime hashes, commands and quotes. Six cases
cover buys/sells with creator tax 0/133 bps, repeated trades, a round trip using
simulated state, reverse execution and overstated-output rollback. Go quotes
precede deployed Kyber execution; completion, launches, funding and signer
authorization stay inside the fork.

Captured values retain provenance in the
[original evidence branch](https://github.com/yummybaozi/kyberswap-dex-lib/tree/39a2df390ed33dd8e8c56fc80072ec7961b8a005).
Historical reverse inputs are replayed as exact-input executions; the new model's
minimum-input property is tested separately.
