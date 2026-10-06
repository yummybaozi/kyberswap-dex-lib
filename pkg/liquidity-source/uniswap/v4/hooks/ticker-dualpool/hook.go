package tickerdualpool

import (
	"context"
	_ "embed"
	"errors"
	"math/big"
	"strings"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/goccy/go-json"
	"github.com/holiman/uint256"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/dualpool"
)

//go:embed abis/Ticker.json
var tickerABIJSON string
var tickerABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(tickerABIJSON))
	if err != nil {
		panic(err)
	}
	return a
}()
var ErrRefreshRequired = errors.New("Ticker vault state must be refreshed after simulated JIT teardown")

type Snapshot struct {
	State
	Vaults      [2]common.Address
	BlockNumber uint64
	// Supplied from measured execution, not borrowed from Twofold's gas estimate.
	SwapGas int64
}

type Hook struct {
	uniswapv4.BaseHook
	Snapshot
	address         common.Address
	decodeErr       error
	refreshRequired bool
}

var _ uniswapv4.Hook = (*Hook)(nil)

// NewHook is explicitly registered by local fixtures. No production Ticker
// address is registered until the deployment and bytecode have been verified.
func NewHook(p *uniswapv4.HookParam) uniswapv4.Hook {
	h := &Hook{address: p.HookAddress}
	if len(p.HookExtra) > 0 {
		h.decodeErr = p.HookExtra.Unmarshal(&h.Snapshot)
	}
	return h
}
func (h *Hook) GetExchange() string               { return "uniswap-v4-ticker-dualpool" }
func (h *Hook) AllowEmptyTicks() bool             { return true }
func (h *Hook) CanBeforeSwap(common.Address) bool { return true }
func (h *Hook) CanAfterSwap(common.Address) bool  { return false }

func (h *Hook) BeforeSwap(p *uniswapv4.BeforeSwapParams) (*uniswapv4.BeforeSwapResult, error) {
	if h.decodeErr != nil {
		return nil, h.decodeErr
	}
	if h.refreshRequired {
		return nil, ErrRefreshRequired
	}
	if p.AmountSpecified == nil || p.AmountSpecified.Sign() <= 0 {
		return nil, ErrInvalidState
	}
	amount, overflow := uint256.FromBig(p.AmountSpecified)
	if overflow {
		return nil, ErrInvalidState
	}
	var q Quote
	var err error
	if p.CalcOut {
		q, err = QuoteExactInput(h.State, p.ZeroForOne, amount)
	} else {
		q, err = QuoteRequiredInput(h.State, p.ZeroForOne, amount)
	}
	if err != nil {
		return nil, err
	}
	result := &uniswapv4.BeforeSwapResult{SwapFee: uniswapv4.FeeAmount(h.State.Pool.LpFee), Gas: h.SwapGas, SwapInfo: q}
	if p.CalcOut {
		result.DeltaSpecified = q.Swap.AmountIn.ToBig()
		result.DeltaUnspecified = new(big.Int).Neg(q.NetOutput.ToBig())
	} else {
		result.DeltaSpecified = new(big.Int).Neg(p.AmountSpecified)
		result.DeltaUnspecified = q.Swap.AmountIn.ToBig()
	}
	return result, nil
}

func (h *Hook) UpdateBalance(info any) {
	q, ok := info.(Quote)
	if !ok || q.Swap.Balance0 == nil || q.Swap.Balance1 == nil {
		return
	}
	h.State.Pool.Balance0 = q.Swap.Balance0.Clone()
	h.State.Pool.Balance1 = q.Swap.Balance1.Clone()
	h.Total0 = q.Swap.Balance0.Clone()
	h.Total1 = q.Swap.Balance1.Clone()
	h.State.Pool.SqrtPriceX96 = q.Swap.SqrtPriceX96.Clone()
	h.State.Pool.Tick = q.Swap.Tick
	// JIT arithmetic alone does not model an ERC-4626 withdrawal/redeposit.
	// Keep this unfinished path explicit rather than publish inaccurate quotes.
	h.refreshRequired = h.Vaults[0] != (common.Address{}) || h.Vaults[1] != (common.Address{})
}

func (h *Hook) CloneState() uniswapv4.Hook {
	clone := *h
	copyInt := func(v *uint256.Int) *uint256.Int {
		if v == nil {
			return nil
		}
		return v.Clone()
	}
	clone.Total0 = copyInt(h.Total0)
	clone.Total1 = copyInt(h.Total1)
	clone.Unbacked0 = copyInt(h.Unbacked0)
	clone.Unbacked1 = copyInt(h.Unbacked1)
	clone.State.Pool.Balance0 = copyInt(h.State.Pool.Balance0)
	clone.State.Pool.Balance1 = copyInt(h.State.Pool.Balance1)
	clone.State.Pool.SqrtPriceX96 = copyInt(h.State.Pool.SqrtPriceX96)
	clone.State.Pool.Buckets = append([]dualpool.Bucket(nil), h.State.Pool.Buckets...)
	return &clone
}
func (h *Hook) GetReserves(context.Context, *uniswapv4.HookParam) (entity.PoolReserves, error) {
	if h.State.Pool.Balance0 == nil || h.State.Pool.Balance1 == nil {
		return nil, ErrInvalidState
	}
	return entity.PoolReserves{h.State.Pool.Balance0.Dec(), h.State.Pool.Balance1.Dec()}, nil
}

type launchResponse struct {
	Registered, MemecoinIsCurrency0                                                           bool
	Memecoin, QuoteToken, Creator, BuybackCreatorRecipient, ProtocolFeeRecipient              common.Address
	CreatorTaxBps, ProtocolFeeShareBps, BuybackBurnBps, HookFeeBps, MaxInternalPriceImpactBps uint16
	BuybackEnabled                                                                            bool
}

func (h *Hook) Track(ctx context.Context, p *uniswapv4.HookParam) (json.RawMessage, error) {
	if p.BlockNumber == nil || p.BlockNumber.Sign() <= 0 || p.Cfg == nil || p.Cfg.StateViewAddress == "" || p.Pool == nil || len(p.Pool.Tokens) != 2 {
		return nil, ErrInvalidState
	}
	var config uniswapv4.StaticExtra
	if err := json.Unmarshal([]byte(p.Pool.StaticExtra), &config); err != nil {
		return nil, err
	}
	if config.HooksAddress != h.address {
		return nil, ErrInvalidState
	}
	key := uniswapv4.PoolKey{Fee: big.NewInt(int64(config.Fee)), TickSpacing: big.NewInt(int64(config.TickSpacing)), Hooks: h.address}
	currencies := [2]common.Address{common.HexToAddress(p.Pool.Tokens[0].Address), common.HexToAddress(p.Pool.Tokens[1].Address)}
	for i, native := range config.IsNative {
		if native {
			currencies[i] = common.Address{}
		}
	}
	key.Currency0 = currencies[0]
	key.Currency1 = currencies[1]
	id := common.HexToHash(p.Pool.Address)
	// Both requests use the block chosen by the parent v4 tracker, and the same overrides.
	raw, err := dualpool.NewStateReader(h.address).Track(ctx, p)
	if err != nil {
		return nil, err
	}
	next := Snapshot{BlockNumber: p.BlockNumber.Uint64(), SwapGas: h.SwapGas}
	if err = json.Unmarshal(raw, &next.State.Pool); err != nil {
		return nil, err
	}
	var total struct{ Amount0, Amount1 *big.Int }
	var launch launchResponse
	req := p.RpcClient.NewRequest().SetContext(ctx).SetBlockNumber(p.BlockNumber).SetOverrides(p.Overrides)
	target := hexutil.Encode(h.address[:])
	req.AddCall(&ethrpc.Call{ABI: tickerABI, Target: target, Method: "totalAssets", Params: []any{key}}, []any{&total})
	req.AddCall(&ethrpc.Call{ABI: tickerABI, Target: target, Method: "launches", Params: []any{id}}, []any{&launch})
	for i := range currencies {
		req.AddCall(&ethrpc.Call{ABI: tickerABI, Target: target, Method: "vaults", Params: []any{id, currencies[i]}}, []any{&next.Vaults[i]})
	}
	if _, err = req.Aggregate(); err != nil {
		return nil, err
	}
	if total.Amount0 == nil || total.Amount1 == nil {
		return nil, ErrInvalidState
	}
	next.Total0 = uint256.MustFromBig(total.Amount0)
	next.Total1 = uint256.MustFromBig(total.Amount1)
	next.TickSpacing = int(config.TickSpacing)
	next.Registered = launch.Registered
	next.HookFeeBps = launch.HookFeeBps
	next.CreatorTaxBps = launch.CreatorTaxBps
	h.Snapshot = next
	h.decodeErr = nil
	h.refreshRequired = false
	return json.Marshal(next)
}
