// Package tickermemehook prices swaps on Ticker's TickerMemeHook on Monad, the
// Uniswap V4 hook of every graduated Ticker launch that is not routed to the
// DualPool venue. The contract is Ticker's build of Pons' PonsV2MemeHook
// (hooks/pons-v2) with a different fee sweep: buyback-and-burn instead of
// buyback-and-lock, and a 60% ceiling on the protocol share. Nothing a swap
// sees differs. The pool is an
// ordinary V4 pool with resident liquidity, and the hook's only swap-time act is
// _afterSwap: hookFeeBps and creatorTaxBps of the unspecified currency, each
// floored on its own, both frozen per pool at registerPool time and read through
// the `launches` getter.
//
// This package does not embed ponsv2.Extra. That floors the two parts as one
// combined rate, which is one unit above the contract whenever both parts have a
// fractional unit, and on CalcAmountIn it takes the fee from the input, the
// contract's exact-out branch, where the router executes exact-in. Here the two
// parts are floored separately and CalcAmountIn grosses the requested output up in
// BeforeSwap, so the input it finds delivers at least the request.
package tickermemehook

import (
	"context"
	"math/big"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/goccy/go-json"

	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/bignumber"
)

// Extra is the pool's frozen terms from launches(poolId), persisted in HookExtra.
type Extra struct {
	Registered    bool   `json:"reg"`
	HookFeeBps    uint16 `json:"hF"`
	CreatorTaxBps uint16 `json:"cT"`
}

type Hook struct {
	uniswapv4.Hook `json:"-"`
	Extra
	decodeErr error
}

var _ uniswapv4.Hook = (*Hook)(nil)

// NewHook is explicitly registered by tests; production address registration stays disabled.
func NewHook(param *uniswapv4.HookParam) uniswapv4.Hook {
	h := &Hook{Hook: &uniswapv4.BaseHook{Exchange: Exchange}}
	if len(param.HookExtra) > 0 {
		h.decodeErr = param.HookExtra.Unmarshal(&h.Extra)
	}
	return h
}

// Track reads the pool's terms from the hook's `launches` getter. Both bps are
// frozen at registerPool time, so a pool seen registered is never re-fetched.
func (h *Hook) Track(ctx context.Context, param *uniswapv4.HookParam) (json.RawMessage, error) {
	if h.decodeErr != nil {
		return nil, h.decodeErr
	}
	if h.Registered {
		return json.Marshal(h.Extra)
	}
	var info launchInfo
	req := param.RpcClient.NewRequest().SetContext(ctx).SetOverrides(param.Overrides)
	if param.BlockNumber != nil && param.BlockNumber.Sign() > 0 {
		req.SetBlockNumber(param.BlockNumber)
	}
	if _, err := req.AddCall(&ethrpc.Call{ABI: hookABI, Target: hexutil.Encode(param.HookAddress[:]),
		Method: "launches", Params: []any{common.HexToHash(param.Pool.Address)}}, []any{&info}).Call(); err != nil {
		return nil, err
	}
	h.Extra = Extra{Registered: info.Registered, HookFeeBps: info.HookFeeBps, CreatorTaxBps: info.CreatorTaxBps}
	return json.Marshal(h.Extra)
}

// CanBeforeSwap is true although the hook address carries no beforeSwap bit: the
// simulator only calls BeforeSwap when it is, and CalcAmountIn's inversion of
// the skim lives there.
func (h *Hook) CanBeforeSwap(common.Address) bool { return true }

// BeforeSwap leaves an exact-in quote untouched. CalcAmountIn reverses exact-in,
// and the skim comes off the output, so it asks the pool for a gross output G
// with G*(10000-fee-tax)/10000 >= requested. Each floor only lowers the skim, so
// every gross output >= G nets at least the request.
func (h *Hook) BeforeSwap(params *uniswapv4.BeforeSwapParams) (*uniswapv4.BeforeSwapResult, error) {
	if h.decodeErr != nil {
		return nil, h.decodeErr
	}
	res := &uniswapv4.BeforeSwapResult{DeltaSpecified: bignumber.ZeroBI, DeltaUnspecified: bignumber.ZeroBI}
	keepBps := int64(basisPoints) - int64(h.feeBps()) - int64(h.taxBps())
	if params.CalcOut || keepBps == basisPoints {
		return res, nil
	}
	if keepBps <= 0 {
		return nil, ErrInvalidTerms
	}
	gross := bignumber.MulDivUp(new(big.Int), params.AmountSpecified, big.NewInt(basisPoints), big.NewInt(keepBps))
	res.DeltaSpecified = gross.Sub(gross, params.AmountSpecified)
	return res, nil
}

// AfterSwap mirrors TickerMemeHook._afterSwap for an exact-in swap: the hook fee
// and the creator tax are each floored on the gross output and their sum is taken
// from the output currency. On CalcAmountIn the skim is already inverted in
// BeforeSwap, so nothing is added to the input.
func (h *Hook) AfterSwap(params *uniswapv4.AfterSwapParams) (*uniswapv4.AfterSwapResult, error) {
	if !params.CalcOut {
		return &uniswapv4.AfterSwapResult{HookFee: bignumber.ZeroBI}, nil
	}
	return &uniswapv4.AfterSwapResult{HookFee: h.Skim(params.AmountOut)}, nil
}

// Skim is the amount _afterSwap takes from a gross output: the two parts are
// rounded down separately, as on-chain, not as one combined rate.
func (e *Extra) Skim(gross *big.Int) *big.Int {
	total := new(big.Int)
	if gross == nil || gross.Sign() <= 0 {
		return total
	}
	var part big.Int
	for _, bps := range [2]uint16{e.feeBps(), e.taxBps()} {
		if bps != 0 {
			part.Mul(gross, big.NewInt(int64(bps)))
			total.Add(total, part.Div(&part, big.NewInt(basisPoints)))
		}
	}
	return total
}

// An unregistered pool is not skimmed: _afterSwap returns before any fee math.
func (e *Extra) feeBps() uint16 {
	if !e.Registered {
		return 0
	}
	return e.HookFeeBps
}

func (e *Extra) taxBps() uint16 {
	if !e.Registered {
		return 0
	}
	return e.CreatorTaxBps
}

// CloneState returns the hook itself: the terms never change after Track and a
// swap moves only the pool's own state.
func (h *Hook) CloneState() uniswapv4.Hook { return h }
