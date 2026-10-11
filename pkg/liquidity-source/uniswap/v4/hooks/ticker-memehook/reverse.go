package tickermemehook

import (
	"math/big"

	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
)

var _ uniswapv4.HookWithCalcAmountIn = (*Hook)(nil)

// CalcAmountIn searches gross output, which is monotone. Net output can fall by
// one unit when both separately rounded fees increment at the same gross amount.
// Replaying the chosen input supplies the exact-input fee, gas and post-swap state.
func (h *Hook) CalcAmountIn(param pool.CalcAmountInParams, quote, grossQuote uniswapv4.HookQuoteFunc) (*pool.CalcAmountInResult, error) {
	if _, err := h.BeforeSwap(nil); err != nil {
		return nil, err
	}
	desired := param.TokenAmountOut.Amount
	if desired == nil || desired.Sign() <= 0 || desired.BitLen() > 256 {
		return nil, uniswapv4.ErrInvalidAmountOut
	}
	keep := int64(basisPoints) - int64(h.feeBps()) - int64(h.taxBps())
	// net(g) < g*keep/10000 + 2. No smaller gross amount can reach desired.
	candidate := new(big.Int).Sub(desired, big.NewInt(2))
	if candidate.Sign() < 0 {
		candidate.SetInt64(0)
	}
	candidate.Mul(candidate, big.NewInt(basisPoints)).Div(candidate, big.NewInt(keep)).Add(candidate, big.NewInt(1))
	calcGross := func(input *big.Int) (*big.Int, error) {
		res, err := grossQuote(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: param.TokenIn, Amount: input}, TokenOut: param.TokenAmountOut.Token, Limit: param.Limit})
		if err != nil {
			return nil, err
		}
		return res.TokenAmountOut.Amount, nil
	}
	// Two fee floors differ from their unfloored sum by less than two units.
	// With integer basis points and keep >= 1, at most 20000 gross candidates
	// separate the lower bound above from an output that always reaches desired.
	for attempts := 0; attempts <= 2*basisPoints; attempts++ {
		net := new(big.Int).Sub(candidate, h.Skim(candidate))
		if net.Cmp(desired) < 0 {
			candidate.Add(candidate, big.NewInt(1))
			continue
		}
		input, err := uniswapv4.MinAmountIn(candidate, nil, calcGross)
		if err != nil {
			return nil, err
		}
		res, err := quote(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: param.TokenIn, Amount: input}, TokenOut: param.TokenAmountOut.Token, Limit: param.Limit})
		if err != nil {
			return nil, err
		}
		if res.TokenAmountOut.Amount.Cmp(desired) >= 0 {
			return &pool.CalcAmountInResult{
				TokenAmountIn:           &pool.TokenAmount{Token: param.TokenIn, Amount: input},
				RemainingTokenAmountOut: &pool.TokenAmount{Token: param.TokenAmountOut.Token, Amount: new(big.Int)},
				Fee:                     res.Fee, Gas: res.Gas, SwapInfo: res.SwapInfo,
			}, nil
		}
		// Gross output jumped over the candidate onto a simultaneous fee boundary.
		// Smaller inputs were below the first qualifying candidate; continue above
		// this landing point rather than binary-searching non-monotone net output.
		actual, err := calcGross(input)
		if err != nil {
			return nil, err
		}
		candidate.Add(actual, big.NewInt(1))
	}
	return nil, uniswapv4.ErrAmountOutUnreachable
}
