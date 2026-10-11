package tickermemehook

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
)

func TestReverseQuoteSkipsUnreachableFeeBoundary(t *testing.T) {
	h := &Hook{Extra: Extra{true, 1000, 1000}}
	quote := func(skim bool) uniswapv4.HookQuoteFunc {
		return func(p pool.CalcAmountOutParams) (*pool.CalcAmountOutResult, error) {
			gross := new(big.Int).Mul(p.TokenAmountIn.Amount, big.NewInt(20))
			out := new(big.Int).Set(gross)
			if skim {
				out.Sub(out, h.Skim(gross))
			}
			return &pool.CalcAmountOutResult{TokenAmountOut: &pool.TokenAmount{Token: p.TokenOut, Amount: out}}, nil
		}
	}
	// Gross 19 would net 17, but inputs jump from gross 0 to 20, which nets 16.
	// The next attainable gross is 40: input 2 is the first that reaches 17.
	res, err := h.CalcAmountIn(pool.CalcAmountInParams{TokenIn: token0, TokenAmountOut: pool.TokenAmount{Token: token1, Amount: big.NewInt(17)}}, quote(true), quote(false))
	require.NoError(t, err)
	require.Equal(t, int64(2), res.TokenAmountIn.Amount.Int64())
}

func TestReverseQuoteFindsFirstQualifyingInput(t *testing.T) {
	t.Parallel()
	for _, terms := range []Extra{{true, 0, 0}, {true, 100, 0}, {true, 100, 100}, {true, 100, 133}, {true, 1000, 1000}, {false, 100, 133}} {
		for _, direction := range []bool{false, true} {
			sim := newSim(t, terms, true)
			in, out := token0, token1
			if !direction {
				in, out = out, in
			}
			// Exhaustive forward quotes are an independent oracle. Checking only
			// input-1 is insufficient when separately rounded fees make output fall.
			outputs := make([]*big.Int, 301)
			for input := 1; input < len(outputs); input++ {
				result, err := quoteP(synth, sim, direction, big.NewInt(int64(input)))
				if err == nil {
					outputs[input] = result.TokenAmountOut.Amount
				}
			}
			for desired := int64(1); desired <= 200; desired++ {
				want := big.NewInt(desired)
				var first int
				for input, output := range outputs {
					if output != nil && output.Cmp(want) >= 0 {
						first = input
						break
					}
				}
				require.NotZero(t, first)
				result, err := sim.CalcAmountIn(pool.CalcAmountInParams{TokenAmountOut: pool.TokenAmount{Token: out, Amount: want}, TokenIn: in})
				require.NoError(t, err)
				require.Equal(t, int64(first), result.TokenAmountIn.Amount.Int64(), "terms=%+v direction=%t desired=%d", terms, direction, desired)
				replay, err := quoteP(synth, sim, direction, result.TokenAmountIn.Amount)
				require.NoError(t, err)
				require.Equal(t, replay.SwapInfo, result.SwapInfo)
				require.Equal(t, replay.Fee, result.Fee)
				require.Equal(t, replay.Gas, result.Gas)
				require.Equal(t, want, big.NewInt(desired), "request must not be mutated")
			}
		}
	}
}

func TestReverseQuoteUpdatesLikeExactInput(t *testing.T) {
	t.Parallel()
	sim := newSim(t, Extra{true, 100, 133}, true)
	for _, direction := range []bool{false, true} {
		in, out := token0, token1
		if !direction {
			in, out = out, in
		}
		reverse, err := sim.CalcAmountIn(pool.CalcAmountInParams{TokenAmountOut: pool.TokenAmount{Token: out, Amount: big.NewInt(123456789)}, TokenIn: in})
		require.NoError(t, err)
		forward, err := quoteP(synth, sim, direction, reverse.TokenAmountIn.Amount)
		require.NoError(t, err)
		a, b := sim.CloneState(), sim.CloneState()
		update := pool.UpdateBalanceParams{TokenAmountIn: *reverse.TokenAmountIn, TokenAmountOut: *forward.TokenAmountOut, SwapInfo: reverse.SwapInfo}
		a.UpdateBalance(update)
		update.SwapInfo = forward.SwapInfo
		b.UpdateBalance(update)
		nextA, err := quoteP(synth, a, !direction, forward.TokenAmountOut.Amount)
		require.NoError(t, err)
		nextB, err := quoteP(synth, b, !direction, forward.TokenAmountOut.Amount)
		require.NoError(t, err)
		require.Equal(t, nextB, nextA)
	}
}
