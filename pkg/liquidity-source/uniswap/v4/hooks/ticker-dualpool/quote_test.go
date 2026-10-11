package tickerdualpool

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/dualpool"
)

func fixtureState() State {
	return State{
		Pool: dualpool.Extra{Live: true, Balance0: uint256.MustFromDecimal("1000000000000000000000000"),
			Balance1:     uint256.MustFromDecimal("1000000000000000000000000"),
			SqrtPriceX96: uint256.MustFromDecimal("79228162514264337593543950336"),
			LpFee:        2000, Buckets: []dualpool.Bucket{{TickLower: -10000, TickUpper: 10000, WeightBps: 10000}}},
		Total0:      uint256.MustFromDecimal("1000000000000000000000000"),
		Total1:      uint256.MustFromDecimal("1000000000000000000000000"),
		TickSpacing: 10, Registered: true, HookFeeBps: 100, CreatorTaxBps: 50,
	}
}

// Values come from an executed Ticker hook, not from another Go pricing function.
func TestQuotesMatchTickerSolidity(t *testing.T) {
	raw, err := os.ReadFile("testdata/quotes.json")
	require.NoError(t, err)
	var fixtures []map[string]string
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	for _, f := range fixtures {
		t.Run(f["side"]+"/"+f["input"], func(t *testing.T) {
			s := fixtureState()
			before, _ := json.Marshal(s)
			q, err := QuoteExactInput(s, f["side"] == "0", uint256.MustFromDecimal(f["input"]))
			require.NoError(t, err)
			require.Equal(t, f["net"], q.NetOutput.Dec())
			require.Equal(t, f["after0"], q.Swap.Balance0.Dec())
			require.Equal(t, f["after1"], q.Swap.Balance1.Dec())
			require.Equal(t, f["fee"], q.HookFee.Dec())
			require.Equal(t, f["tax"], q.CreatorTax.Dec())
			require.Equal(t, f["price"], q.Swap.SqrtPriceX96.Dec())
			tick, err := strconv.Atoi(f["tick"])
			require.NoError(t, err)
			require.Equal(t, int32(tick), q.Swap.Tick)
			after, _ := json.Marshal(s)
			require.Equal(t, string(before), string(after), "quoting must not mutate pool state")
		})
	}
}

func TestFloorRejectsShortageAndUnbackedClaims(t *testing.T) {
	for _, which := range []string{"partial", "zero", "unbacked0", "unbacked1"} {
		t.Run(which, func(t *testing.T) {
			s := fixtureState()
			switch which {
			case "partial":
				s.Pool.Balance1.SubUint64(s.Pool.Balance1, 1)
			case "zero":
				s.Pool.Balance1.Clear()
			case "unbacked0":
				s.Unbacked0 = uint256.NewInt(1)
			case "unbacked1":
				s.Unbacked1 = uint256.NewInt(1)
			}
			_, err := QuoteExactInput(s, true, uint256.NewInt(1001))
			require.ErrorIs(t, err, ErrVaultIlliquid)
		})
	}
}

func TestQuotesMatchFreshMonadAaveDeployment(t *testing.T) {
	raw, err := os.ReadFile("testdata/monad-quotes.json")
	require.NoError(t, err)
	var fixtures []map[string]string
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	for _, f := range fixtures {
		t.Run(f["direction"], func(t *testing.T) {
			s := fixtureState()
			s.Pool.Balance0 = uint256.MustFromDecimal(f["before0"])
			s.Pool.Balance1 = uint256.MustFromDecimal(f["before1"])
			s.Total0 = s.Pool.Balance0.Clone()
			s.Total1 = s.Pool.Balance1.Clone()
			s.Pool.SqrtPriceX96 = uint256.MustFromDecimal(f["initialPrice"])
			tick, err := strconv.Atoi(f["initialTick"])
			require.NoError(t, err)
			s.Pool.Tick = int32(tick)
			s.Pool.Buckets = []dualpool.Bucket{{TickLower: 312380, TickUpper: 332380, WeightBps: 10000}}
			q, err := QuoteExactInput(s, f["direction"] == "true", uint256.MustFromDecimal(f["input"]))
			require.NoError(t, err)
			require.Equal(t, f["net"], q.NetOutput.Dec())
			require.Equal(t, f["price"], q.Swap.SqrtPriceX96.Dec())
			tick, err = strconv.Atoi(f["tick"])
			require.NoError(t, err)
			require.Equal(t, int32(tick), q.Swap.Tick)
		})
	}
}

func TestReverseQuoteMatchesExhaustiveSmallInputs(t *testing.T) {
	for _, direction := range []bool{true, false} {
		s := fixtureState()
		// Around both separate fee boundaries, net output is not monotone.
		for _, target := range []uint64{1, 97, 98, 99, 100, 196, 197, 198, 199, 200, 201, 395} {
			var expected uint64
			for input := uint64(1); input < 1000; input++ {
				q, err := QuoteExactInput(s, direction, uint256.NewInt(input))
				if err == nil && q.NetOutput.CmpUint64(target) >= 0 {
					expected = input
					break
				}
			}
			require.NotZero(t, expected)
			q, err := QuoteRequiredInput(s, direction, uint256.NewInt(target))
			require.NoError(t, err)
			require.Equal(t, expected, q.Swap.AmountIn.Uint64(), "target %d", target)
			require.GreaterOrEqual(t, q.NetOutput.Uint64(), target)
		}
	}
}

// Unlike the fork snapshot test, each quote here uses only the balances and price
// computed by the PREVIOUS Go quote. No intermediate Solidity state is injected.
func TestSequentialQuotesMatchSolidity(t *testing.T) {
	raw, err := os.ReadFile("testdata/sequential.json")
	require.NoError(t, err)
	var fixtures []map[string]string
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	s := fixtureState()
	for _, f := range fixtures {
		q, err := QuoteExactInput(s, f["direction"] == "true", uint256.MustFromDecimal(f["input"]))
		require.NoError(t, err)
		require.Equal(t, f["net"], q.NetOutput.Dec())
		require.Equal(t, f["price"], q.Swap.SqrtPriceX96.Dec())
		require.Equal(t, f["after0"], q.Swap.Balance0.Dec())
		require.Equal(t, f["after1"], q.Swap.Balance1.Dec())
		s.Pool.Balance0 = q.Swap.Balance0.Clone()
		s.Pool.Balance1 = q.Swap.Balance1.Clone()
		s.Total0 = s.Pool.Balance0.Clone()
		s.Total1 = s.Pool.Balance1.Clone()
		s.Pool.SqrtPriceX96 = q.Swap.SqrtPriceX96.Clone()
		s.Pool.Tick = q.Swap.Tick
	}
}
