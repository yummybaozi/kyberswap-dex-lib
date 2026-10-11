# Ticker DualPool execution evidence

This archive validates the integration at Go commit
`a5ccad0f99f5b90c45b1dc613d062f4c866573ef`, a single commit based on main
`e3b08f15cd94d9bfafc393c9014e0d1cb787e011`.

- Monad fork block: `112288351`.
- Contracts harness base: `e068291086e8e32dfcba29268536f00b124ca8ef` in `jaybutera/ponsfamily` (access required).
- Harness patch SHA-256: `d0d09c9c96a9aefe84c555768a14501755f3ec1aba7982ce23959cf4bfbf3f8e`.
- [Execution report](kyber-execution.json): exact commits, code pins, commands and quote results.

Each quote tracks the deployed DualPool, TickerVault and Aave state at one block,
then saves and loads the complete V4 simulator through msgpack before quoting.
Kyber's deployed router/executor consumes that quote on the local fork. The six
scenarios cover buy/sell, a round trip, a large trade, repeated trades, two pools
sharing Aave cash and a shortage followed by recovery. These executions use
ERC-20 pairs and Aave USDC. They do not establish native-token or Morpho coverage.

The Go checks passed: 35 packages with tests, four race-test packages, vet and
full build. Msgpack generation stays stable and changed Go files match the
repository's goimports convention. Two 15-second fuzz runs passed 103,702 forward
and 14,340 reverse cases. Regression tests verify rejected snapshots, stale
shared-inventory updates and refresh-required states stay rejected after saving
and loading. Both integrations also pass together after retaining both optional
hook interfaces and regenerating msgpack registration in a separate merge check.

Use fresh checkouts to reproduce:

```sh
git clone --no-checkout https://github.com/yummybaozi/kyberswap-dex-lib.git ticker-dualpool-go
git -C ticker-dualpool-go checkout a5ccad0f99f5b90c45b1dc613d062f4c866573ef
git clone --no-checkout https://github.com/jaybutera/ponsfamily.git ticker-dualpool-harness
git -C ticker-dualpool-harness checkout e068291086e8e32dfcba29268536f00b124ca8ef
git -C ticker-dualpool-harness submodule update --init --recursive
git -C ticker-dualpool-harness apply /absolute/path/to/validation/ticker-dualpool/harness.patch
cd ticker-dualpool-harness
python3 tools/kyber/run-mainnet-go.py \
  --kyber-checkout /absolute/path/to/ticker-dualpool-go \
  --venue ticker-dualpool --fork-block 112288351 \
  --report-file deployments/kyber-dualpool-fresh-review-run.json
```

Foundry, Go and archive RPC access through `https://rpc.monad.xyz` are required.
The quote bridge only uses loopback Anvil. No production transactions are sent.
Forge router gas measurements are not production Monad gas estimates.

This evidence is separate from the integration PR. Production hook registration,
service discovery, route-builder payout/settlement handling and gas calibration
remain deployment work; they are not prerequisites for reviewing the integration.
