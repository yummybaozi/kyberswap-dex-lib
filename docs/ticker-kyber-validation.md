# Ticker hooks: test evidence and Kyber submission review

Reviewed on October 10, 2026 against Kyber's `main` at
`e3b08f15cd94d9bfafc393c9014e0d1cb787e011`. The implementation has passing local
Go checks and twelve deployed-contract fork cases. It is ready to request code
review once the required issue reference and reviewer access to the fork harness
are resolved. This is not production activation approval.

## Scope and pricing

The existing DualPool implementation calculates temporary liquidity, output fees,
available vault backing and shared Aave cash. It rejects a swap when the vault
cannot supply its entire backing: shares worth 1,000 USDC with only 400 USDC
available do not become a tradable 400-USDC quote. This update preserves that
implementation and its gas logic.

Classic launches use `strategy = address(0)` and graduate to TickerMemeHook, with
ordinary V4 liquidity and no DualPool vault. The separate meme model subtracts
hook fees and creator tax from gross output, flooring each charge separately.
Two charges of 1.5 units become 1 + 1. Reverse quotes return enough input to
deliver at least the requested output when executed exact-in; they do not promise
minimal input or validate the contract's genuine exact-output swap mode.

Existing Twofold simulators already stored `*uniswapv4.BaseHook` behind a hook
interface. Reloading that simulator from msgpack could panic because the concrete
type was missing from serialization registration. The shared registration fix
and regression preserve quotes before and after reload and a later state update.

## Review against Kyber requirements

The authoritative requirements are Kyber's
[README](https://github.com/KyberNetwork/kyberswap-dex-lib/blob/e3b08f15cd94d9bfafc393c9014e0d1cb787e011/README.md),
[AGENTS.md](https://github.com/KyberNetwork/kyberswap-dex-lib/blob/e3b08f15cd94d9bfafc393c9014e0d1cb787e011/AGENTS.md)
and [PR template](https://github.com/KyberNetwork/kyberswap-dex-lib/blob/e3b08f15cd94d9bfafc393c9014e0d1cb787e011/.github/pull_request_template.md).
Their content matched the latest upstream files at this review.

| Requirement | Evidence and remaining work |
| --- | --- |
| Feature branch into `KyberNetwork/kyberswap-dex-lib:main` | Use `yummybaozi:codex/ticker-dualpool-submission`. Its upstream base matches current `main`; personal-fork PR #1 is not the upstream PR. |
| Related open issue and prior discussion | No related issue is linked in fork PR #1. Add a Ticker integration issue reference, or obtain a documented maintainer exception, before submitting. Do not use an unrelated issue. |
| DEX background, pricing, documentation and addresses | Covered here and in the [DualPool](ticker-dualpool-integration.md) and [meme-hook](ticker-memehook-integration.md) model docs. |
| Discovery, tracking and simulation | Reuse existing V4 factories rather than add a standalone DEX. DualPool tracking is tested; the meme bridge reads terms and constructs the parent V4 simulator from a pinned snapshot. End-to-end production listing/tracking remains unvalidated. Both production hook factories deliberately remain unregistered. |
| Exact integer fees, pure quotes, state updates and independent clones | Fixture, fee, dust, repeated-quote, accounting, stale-state and clone regressions pass. Meme terms are immutable; mutable V4 pool state is cloned independently. |
| Exchange identifiers and serialization | Both hook identifiers and generated concrete hook bindings exist. The parent `uniswap-v4` simulator is already wired through `pkg/pooltypes`; no new standalone pool type is introduced. BaseHook, Twofold and both Ticker serialization checks pass. |
| Supported reverse-quote semantics | Tests execute the returned input exact-in. Kyber must confirm this calling convention; genuine exact-output routing is outside the evidence. |
| Compact fixtures with nested `testdata/go.mod` | The hook fixture directories have nested modules. Tests read fixtures from disk; full fork chain dumps are not shipped. |
| Gas estimate reflects execution | DualPool retains its measured local routing heuristic; meme uses existing parent V4 gas accounting. Final Monad gas calibration remains open. Saved Forge gas values are not production estimates. |
| PR description and release notes | State both hooks, the serialization fix, tests and activation gates using the template. Fork PR #1's current body still describes six DualPool cases and excludes meme coverage; its owner must refresh it. |
| Reproducible deployed-contract evidence | Public JSON records quotes, commands, code hashes and source commits. The companion harness repository is private; arrange reviewer access or an explicitly approved public reproduction package. |

Missing issue/reviewer access are submission preparation gaps. Production
discovery, registration, route construction, service signing and gas calibration
are separate activation gates and must be disclosed, not described as passed.
The README points on-chain encoding contributions to `ks-dex-adapter-lib`; this
branch adds Go models and a test bridge. The existing deployed V4 adapter executes
the tested routes, but this does not validate Kyber's production route builder.

## Exact source and execution checkpoints

| Item | Commit/block |
| --- | --- |
| Kyber base | `e3b08f15cd94d9bfafc393c9014e0d1cb787e011` |
| Go code used in all twelve cases | `82fba4445d0169a1370d1cf276a024a2f353b406` |
| Go evidence checkpoint | `5b5011c792dd775a4239ff0d330b9e596b316ef8` |
| Harness code used in all twelve cases | `eb8284b48e8c5b3f8737d8f0029eb6203f96e97f` |
| Harness evidence checkpoint | `4e1c2c0c107ef8793d1071902bb47503f5eae634` |
| Selectively ported staging source | `06da3581f80a80439a36fb3aa4bf5bc19493b54c` |
| Monad chain / initial fork block | `143` / `112288351` |

The Go evidence checkpoint and this documentation update change no tested code.
The saved run reports a clean Go worktree. Its harness diff at run start contained
only a generated probe report; test/tool source matches the recorded harness code
commit. Those details are retained rather than replacing the tested commits with
later documentation revisions.

All results below are from the October 10, 2026 validation run. This report does
not claim the fork cases were rerun during the documentation review. Tools
available at review were Go `1.25.10` on macOS arm64, Forge/Anvil `1.5.1-stable`
(`b0a9dd9ceda36f63e2326ce530c10e6916f4b8a2`), Python `3.9.6`, and Solidity
`0.8.26` with Cancun, optimizer 200 and IR enabled in the companion harness.

## Offline Go checks

From the Go repository root:

```sh
CI=true go test ./pkg/liquidity-source/uniswap/v4/... ./pkg/msgpack ./pkg/pooltypes ./pkg/test -count=1
CI=true go test -race ./pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook ./pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool ./pkg/liquidity-source/uniswap/v4/hooks/dualpool -count=1
go generate ./pkg/msgpack/...
```

Results: **36 tested packages passed** in the first command; **all three packages
passed** race checks. Packages without tests are not counted as passed tests.
Serialization generation left source unchanged. On the documentation review,
Kyber CI's broader `go generate ./...` also passed and left the worktree clean.
These are targeted checks, not a claim that every repository/service CI job ran.

`CI=true` skips existing remote tests and bridge tests without their explicit local
snapshot environment. The fork runner enables those bridges separately; a skipped
offline bridge is not evidence of executor success.

| Test group | Representative tests/files |
| --- | --- |
| DualPool pricing, reverse search, rounding and changing ranges | `ticker-dualpool/quote_test.go`, `ranges_test.go`, and `dualpool/quote_validation_test.go` replay Solidity and saved Monad fixtures, including sequential swaps and exhaustive small reverse inputs. |
| Available backing, unpaid claims and settlement | `TestFloorRejectsShortageAndUnbackedClaims`, `TestUnpaidClaimsCannotBeRedeemed`, `TestRawInventoryAndManagerCashMoveTogether`. |
| Aave limits and accounting | `aave_test.go`, `aave_reserve_test.go`, `aave_accrual_test.go`, `protocol_test.go`: share/cash floors, frozen/paused states, deposit-cap/treasury effects, sequential accounting and directional fees. These are Aave checks, not Morpho checks. |
| Shared inventory and block consistency | `TestAnotherPoolsSwapReducesSharedAaveCash`, `TestSharedInventoryRejectsStaleQuotesAndMixedBlocks`, `TestParentSimulatorPassesSharedInventory`. |
| Tracker and deployed bytecode recognition | `TestBaseTrackerDecodesPinnedSnapshot`, `recognition_test.go`: reviewed runtime constants/metadata are distinguished from changed executable instructions. |
| Meme output fees, dust and reverse quotes | `TestSkim_RoundsEachPartSeparately`, `TestQuote_ExactIn_SkimsGrossOutput`, `TestCalcAmountIn_RequoteDeliversRequestedOutput`, `TestFixtures_Replay`. Historical staging fixtures are separate from the twelve deployed-foundation cases. |
| Pure quoting, clone independence and invalid terms | `TestCloneStateAndRepeatedQuotesAreIndependent`, `TestCloneAccountingDoesNotSharePointers`, `TestMalformedTermsAreRejected`, `TestProductionAddressesAreUnregistered`. |
| Serialization | `TestMsgpackRoundTrip_PoolSimulator`, `pkg/msgpack/ticker_dualpool_test.go`, and `TestTwofoldSerializationPreservesQuotes`. |

The machine-readable [Go validation report](../pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook/testdata/validation.json)
records commands, exit codes and log SHA-256 hashes.

## Twelve deployed-contract fork cases

Go supplies each quote first. The deployed Kyber router calls its deployed
executor/V4 adapter, which calls PoolManager and the retained Ticker hook.
For DualPool, the hook also calls its fork-created vault/strategy using Aave.
For meme, the hook charges output fees on ordinary V4 liquidity. Expected output
does not come from a reference swap.

The fixture completes the foundation, launches tokens, funds traders, registers
pools and authorizes a test signer locally. It retains both deployed hook proxies,
their implementation pointers and code, and deployed Kyber code. Upstream RPC
receives reads only. The Go reader uses a disposable loopback Anvil with the
exported block/timestamp/storage snapshot. Tests run serially because scratch
files are shared. The bridge defaults omitted `venue` to DualPool.

**Result: 12 passed, 0 failed, 0 skipped.** Each row is one executed Forge test.

| Venue / exact Forge test name | What was checked |
| --- | --- |
| DualPool `test_GoDeployedFoundationBuyAndSell` | 10-USDC buy and half-output sale; full payout from cold executor balances. |
| DualPool `test_GoDeployedFoundationRoundTrip` | Buy and full-output sale in one transaction; second quote uses updated simulated state. |
| DualPool `test_GoDeployedFoundationLargeTrade` | 5,000-USDC buy and sale of all output against roughly 10,000 USDC initial backing. |
| DualPool `test_GoDeployedFoundationRepeatedTrades` | Buy/sell cycles with 1, 100 and 1,000 USDC buys and changing pool state. |
| DualPool `test_GoDeployedFoundationSharedAaveRoute` | Two pools use one Aave cash inventory; the second quote sees the first swap's cash use. |
| DualPool `test_GoDeployedFoundationSharedAaveShortageAndRecovery` | Second pool rejects insufficient cash; attempted whole route rolls back; restoring cash allows exact execution. |
| Meme `test_GoDeployedMemeBuyAndSellWithoutTax` | Buy/sell with creator tax 0 bps. |
| Meme `test_GoDeployedMemeBuyAndSellWithTax` | Buy/sell with creator tax 133 bps (1.33%). |
| Meme `test_GoDeployedMemeRepeatedTrades` | 1/100/1,000-USDC buy/sell cycles with tax. |
| Meme `test_GoDeployedMemeRoundTrip` | Both quotes computed before executing one buy/sell transaction; simulation supplies the second prestate. |
| Meme `test_GoDeployedMemeReverseQuotesBothDirections` | Returned input executes exact-in and delivers at least the requested token/USDC output. |
| Meme `test_GoDeployedMemeRejectsOverstatedQuote` | Output overstated by one raw unit reverts; balances, fees, taxes and pool state restore, including spare executor funds. |

Successful single swaps compare complete input consumption, output, hook fee and
creator tax exactly. DualPool additionally compares vault reserves, price and
shared cash behavior. Meme compares square-root price, tick and liquidity, with
fees/taxes per currency. Round trips compare aggregate balances, both legs' fee
accounting and the second quote's final state.

The public [complete executor report](../pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook/testdata/mainnet-executor.json)
contains all twelve cases, raw amounts, commands, source provenance and runtime
code hashes. The older [DualPool-only report](../pkg/liquidity-source/uniswap/v4/hooks/ticker-dualpool/testdata/mainnet-executor.json)
is an earlier checkpoint, not the current twelve-case report.

### Retained deployed addresses

| Contract | Monad address |
| --- | --- |
| DualPool hook proxy | `0x047ea2f55148b7601fdde0d28588b55c09192ac4` |
| DualPool implementation | `0x844a14bae1b67df6064bb87ba5ff941dc02649e0` |
| Meme hook proxy | `0x223f8b1b43aeec86d2659cce36169f72f7fc2044` |
| Meme implementation | `0x2d96e4e89d37551ff55eb9cfead78da1278d5986` |
| Kyber router | `0x6131b5fae19ea4f9d964eac0408e4408b66337b5` |
| Kyber executor | `0x8f10b468b06c6fd214b65f87778827f7d113f996` |
| Kyber implementation | `0x4f1423cce26c09fb31d96eafe702fc5e22421f6a` |
| V4 adapter | `0x069c11bfc769be175bfae2d20bddce34e46514cc` |
| V4 callback | `0x7b0e2e8300899b647d5ebc66f9d4fa3f16c54061` |

Exact runtime hashes are in the report's `codePins`. The Kyber pin file was
originally captured at block 111136692; those same hashes are checked again in
the 112288351 fork snapshots. Ticker proxy/implementation pins use 112288351.

### Reproduce the fork run

The [companion #199 harness](https://github.com/jaybutera/ponsfamily/pull/199) is
private. The following commands require access; JSON evidence alone does not
provide all Solidity fixture/build dependencies. Do not describe reproduction
as publicly available until reviewers have access or an approved public package.

Use fresh directories and an archive RPC serving the pinned block:

```sh
git clone https://github.com/yummybaozi/kyberswap-dex-lib.git /tmp/ticker-kyber-review
git -C /tmp/ticker-kyber-review checkout 5b5011c792dd775a4239ff0d330b9e596b316ef8
git clone https://github.com/jaybutera/ponsfamily.git /tmp/ticker-kyber-harness
git -C /tmp/ticker-kyber-harness checkout 4e1c2c0c107ef8793d1071902bb47503f5eae634
git -C /tmp/ticker-kyber-harness submodule update --init --recursive
cd /tmp/ticker-kyber-harness
python3 tools/kyber/run-mainnet-go.py --kyber-checkout /tmp/ticker-kyber-review --fork-block 112288351
```

The default upstream RPC is `https://rpc.monad.xyz`; use `--fork-url <archive-rpc>`
if necessary. The runner sets `FOUNDRY_PROFILE=fast`, limits the test directory to
`test/kyber-mainnet`, enables FFI and runs exactly one named case per Forge call.
It writes `deployments/kyber-mainnet-go-run.json`. Its complete per-case Forge
arguments are saved in the report. No private signing key is required.

### Gas and coverage limits

Meme single buy/sell calls used 239,415/215,957 Forge gas with no creator tax and
259,238/235,855 with 133-bps tax. These are test-router call measurements under
Forge's EVM accounting, not production Monad estimates or billing. Other captured
meme call measurements are saved per case; empty arrays mean that case did not
emit a router-call measurement. DualPool's existing local gas fixtures and
heuristic are documented separately in its model doc.

Full parent production pool discovery/tracking, genuine exact-output swaps,
native-token fork coverage, Morpho/arbitrary strategies, production service
signing, shared-inventory route construction and live gas calibration were not
validated by these twelve cases. Both deployed Ticker addresses remain
unregistered in production. A fork-only pool cannot be discovered by Kyber's
public service. No new PR, merge, deployment or mainnet transaction is part of
this documentation review.
