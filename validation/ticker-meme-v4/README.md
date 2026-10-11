# Consolidated TickerMemeHook execution evidence

This archive validates the `ticker-meme-v4` integration at Go commit
`087aaffa61b5fdd2d8538e2bad4700e4e9f66d6b`. It contains execution evidence and an adapter patch,
separate from the integration PR. This revision rechecks the atomic JSON decoding
fix and records successful Go unit, race, vet and full-build checks.

- Monad fork block: `112288351`.
- Contracts harness base: `e068291086e8e32dfcba29268536f00b124ca8ef` in `jaybutera/ponsfamily` (access required).
- Harness patch SHA-256: `1bfcc7f50b660e33cf098d2bd6fc7eb25d0c216698b6aa7b4ca66e1ce9f17222`.
- [Execution report](kyber-execution.json): code pins, exact quotes, commands and per-scenario results.

The patch selects the consolidated hook package and provides Monad tracking
configuration. Each quote first tracks the deployed proxy and then saves/loads
the full simulator through msgpack. The deployed Kyber router/executor executes
the resulting quote on the local fork. Assertions cover exact output, consumed
input, both fee ledgers, price/tick/liquidity, repeated trades, a two-hop round
trip, reverse quotes and rollback when output is overstated by one unit.

Use fresh checkouts to reproduce:

```sh
git clone --no-checkout https://github.com/yummybaozi/kyberswap-dex-lib.git ticker-meme-go
git -C ticker-meme-go checkout 087aaffa61b5fdd2d8538e2bad4700e4e9f66d6b
git clone --no-checkout https://github.com/jaybutera/ponsfamily.git ticker-meme-harness
git -C ticker-meme-harness checkout e068291086e8e32dfcba29268536f00b124ca8ef
git -C ticker-meme-harness submodule update --init --recursive
git -C ticker-meme-harness apply /absolute/path/to/validation/ticker-meme-v4/harness.patch
cd ticker-meme-harness
python3 tools/kyber/run-mainnet-go.py \
  --kyber-checkout /absolute/path/to/ticker-meme-go \
  --venue ticker-memehook --fork-block 112288351 \
  --report-file deployments/kyber-meme-consolidated-run.json
```

Foundry, Go and archive access through `https://rpc.monad.xyz` are required. The
runner uses a loopback Anvil snapshot. It sends no production transactions.
Router gas values are Forge measurements; production Monad gas needs calibration.

The six Kyber scenarios use ERC-20 pairs. Native-token PoolManager coverage is
retained in the integration's sixteen inline captured swaps, whose original
[harness and raw captures](https://github.com/yummybaozi/kyberswap-dex-lib/tree/6849e5efe04f29a51f4c2885eb05ac040adeaf14/pkg/liquidity-source/uniswap/v4/hooks/ticker-meme-v4/testdata)
are pinned separately at block `112361144`. These two execution layers are
reported separately.
