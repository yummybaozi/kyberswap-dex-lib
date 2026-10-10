# TickerMemeHook integration

Ticker's classic launches graduate into an ordinary Uniswap V4 pool. The V4
simulator already prices its liquidity and updates its price and tick. This
hook model reads frozen launch terms and subtracts the hook fee and creator
tax from the swap output, rounding each component down separately.

For example, gross output of 150 units with a 1% hook fee and 1% creator tax
pays 148 units: each 1.5-unit charge rounds down to 1. Combining the rates
would incorrectly charge 3 units. Unregistered pools have no hook skim.

`CalcAmountIn` reverses exact-input execution. It conservatively grosses up
requested output before invoking V4's reverse calculation. Executing the
returned input must deliver at least the request; the input is not promised
to be minimal. Genuine exact-output routing is not tested by this model.

## Registration and serialization

`NewHook` is exported for explicit fixture registration. Neither Ticker's
DualPool nor meme proxy is automatically registered for production.
`ExchangeUniswapV4TickerMemeHook` names the model `uniswap-v4-ticker-memehook`;
pool type, discovery and base simulation remain the existing `uniswap-v4`.
Tracked hook JSON contains `reg`, `hF` and `cT`. Malformed terms are rejected.
Registered terms are immutable; later tracker refreshes reuse them.

The common `*uniswapv4.BaseHook` concrete type is now registered with msgpack.
Existing Twofold hooks held that value behind an interface; previously saving
and reloading their simulators panicked. A Twofold regression compares the
original and decoded quotes, including a subsequent state update.

## Tests and reproduction

The fee model, ABI and historical fixtures were selectively ported from
jaybutera/kyberswap-dex-lib-staging commit
`06da3581f80a80439a36fb3aa4bf5bc19493b54c`. Its simpler DualPool model and
transaction-sending live test were not ported. Historical meme fixtures are
separate from validation of the deployed foundation.

Run offline checks with:

```sh
CI=true go test ./pkg/liquidity-source/uniswap/v4/... ./pkg/msgpack ./pkg/pooltypes ./pkg/test -count=1
CI=true go test -race ./pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool ./pkg/liquidity-source/uniswap/v4/hooks/dualpool -count=1
```

From the companion contracts harness in jaybutera/ponsfamily PR #199:

```sh
python3 tools/kyber/run-mainnet-go.py --kyber-checkout /absolute/path/to/kyberswap-dex-lib --fork-block 112288351
```

The local runner tests both venues, defaults older bridge requests to DualPool,
and runs serially because snapshot files are shared. For meme pools it reads
launch terms, slot0, liquidity and the two graduation ticks at one block and
constructs the parent V4 simulator. It validates the single full-range position;
this bridge is not production pool discovery or the full parent tracker pipeline.

Go quotes precede execution through the deployed Monad Kyber router and
executor. Completion, USDC classic launches (strategy zero), funding and signer
authorization are fork-only. Snapshot reads use a disposable loopback Anvil;
the upstream RPC receives reads only. Proxy addresses, implementations and
Kyber runtime hashes are retained and checked. Cases cover tax 0/133 bps,
repeated trades, simulated one-transaction round trips, reverse quotes in both
directions, and rejection/rollback for an output overstated by one unit.

Meme response evidence records input, output, hook fee, creator tax, square-root
price, tick and liquidity. It does not reuse DualPool vault reserve assertions.
Recorded Forge router-call gas is not a production Monad gas estimate.
Production address activation, gas calibration and service-generated routes
remain outstanding; Morpho and native-token fork coverage are outside this run.
