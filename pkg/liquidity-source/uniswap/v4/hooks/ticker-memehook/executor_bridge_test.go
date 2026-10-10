package tickermemehook

import (
	"context"
	"encoding/json"
	"math/big"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	v4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

const bridgeStateView = "0x77395F3b2E73aE90843717371294fa97cC419D64"
const bridgeStateABI = `[
 {"type":"function","name":"getSlot0","stateMutability":"view","inputs":[{"type":"bytes32"}],"outputs":[{"type":"uint160","name":"sqrtPriceX96"},{"type":"int24","name":"tick"},{"type":"uint24","name":"protocolFee"},{"type":"uint24","name":"lpFee"}]},
 {"type":"function","name":"getLiquidity","stateMutability":"view","inputs":[{"type":"bytes32"}],"outputs":[{"type":"uint128"}]},
 {"type":"function","name":"getTickInfo","stateMutability":"view","inputs":[{"type":"bytes32"},{"type":"int24"}],"outputs":[{"type":"uint128","name":"liquidityGross"},{"type":"int128","name":"liquidityNet"},{"type":"uint256","name":"feeGrowthOutside0X128"},{"type":"uint256","name":"feeGrowthOutside1X128"}]}
]`

type bridgeRequest struct {
	Venue, Hook, Token0, Token1, PoolID, TokenIn, AmountIn, DesiredOut string
	Fee                                                                uint32
	TickSpacing                                                        int
	Block, Timestamp                                                   uint64
	RoundTrip                                                          bool
}

// TestExecutorBridge only reads a loopback Anvil holding the Forge snapshot.
// Its quotes are consumed by the Solidity test before Kyber executes any swap.
func TestExecutorBridge(t *testing.T) {
	path := os.Getenv("KYBER_BRIDGE_REQUEST")
	if path == "" {
		t.Skip("requires local executor harness")
	}
	u, err := url.Parse(os.Getenv("KYBER_LOCAL_RPC"))
	require.NoError(t, err)
	require.True(t, u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback(), "local RPC only")
	var m bridgeRequest
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, "ticker-memehook", m.Venue)
	client := ethrpc.New(u.String()).SetMulticallContract(common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))
	header, err := client.GetETHClient().HeaderByNumber(context.Background(), new(big.Int).SetUint64(m.Block))
	require.NoError(t, err)
	require.Equal(t, m.Timestamp, header.Time)
	chain, err := client.GetETHClient().ChainID(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(143), chain.Int64())
	sim, plain, terms := bridgeSimulator(t, client, m)
	in := strings.ToLower(m.TokenIn)
	out := strings.ToLower(m.Token1)
	if strings.EqualFold(in, m.Token1) {
		out = strings.ToLower(m.Token0)
	}
	amount, ok := new(big.Int).SetString(m.AmountIn, 10)
	require.True(t, ok)
	if m.DesiredOut != "" && m.DesiredOut != "0" {
		desired, ok := new(big.Int).SetString(m.DesiredOut, 10)
		require.True(t, ok)
		reverse, err := sim.CalcAmountIn(pool.CalcAmountInParams{TokenAmountOut: pool.TokenAmount{Token: out, Amount: desired}, TokenIn: in})
		require.NoError(t, err)
		amount = reverse.TokenAmountIn.Amount
	}
	quote := func(s, p *v4.PoolSimulator, tokenIn, tokenOut string, input *big.Int) (*pool.CalcAmountOutResult, map[string]string) {
		params := pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: tokenIn, Amount: input}, TokenOut: tokenOut}
		result, err := s.CalcAmountOut(params)
		require.NoError(t, err)
		gross, err := p.CalcAmountOut(params)
		require.NoError(t, err)
		fee, tax := new(big.Int), new(big.Int)
		if terms.Registered {
			fee.Div(fee.Mul(gross.TokenAmountOut.Amount, big.NewInt(int64(terms.HookFeeBps))), big.NewInt(basisPoints))
			tax.Div(tax.Mul(gross.TokenAmountOut.Amount, big.NewInt(int64(terms.CreatorTaxBps))), big.NewInt(basisPoints))
		}
		require.Equal(t, new(big.Int).Sub(gross.TokenAmountOut.Amount, new(big.Int).Add(fee, tax)).String(), result.TokenAmountOut.Amount.String())
		si := result.SwapInfo.(v4.SwapInfo)
		consumed := new(big.Int).Set(input)
		if result.RemainingTokenAmountIn != nil {
			consumed.Sub(consumed, result.RemainingTokenAmountIn.Amount)
		}
		require.Equal(t, input.String(), consumed.String(), "fork scenarios consume full input")
		return result, map[string]string{"input": consumed.String(), "output": result.TokenAmountOut.Amount.String(), "hookFee": fee.String(), "creatorTax": tax.String(), "sqrtPriceX96": si.NextStateSqrtRatioX96.Dec(), "tick": big.NewInt(int64(si.NextStateTickCurrent)).String(), "liquidity": si.NextStateLiquidity.Dec()}
	}
	first, values := quote(sim, plain, in, out, amount)
	if m.DesiredOut != "" && m.DesiredOut != "0" {
		desired, _ := new(big.Int).SetString(m.DesiredOut, 10)
		require.GreaterOrEqual(t, first.TokenAmountOut.Amount.Cmp(desired), 0)
	}
	if m.RoundTrip {
		update := pool.UpdateBalanceParams{TokenAmountIn: pool.TokenAmount{Token: in, Amount: amount}, TokenAmountOut: *first.TokenAmountOut, SwapInfo: first.SwapInfo}
		sim.UpdateBalance(update)
		// Price and liquidity come from the same SwapInfo; reserve estimates are not used to price.
		plain.UpdateBalance(update)
		_, second := quote(sim, plain, out, in, first.TokenAmountOut.Amount)
		for k, v := range second {
			values["second_"+k] = v
		}
	}
	response, err := json.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("KYBER_BRIDGE_RESPONSE"), response, 0600))
}

func bridgeSimulator(t *testing.T, client *ethrpc.Client, m bridgeRequest) (*v4.PoolSimulator, *v4.PoolSimulator, Extra) {
	t.Helper()
	address := common.HexToAddress(m.Hook)
	old, existed := v4.HookFactories[address]
	v4.RegisterHooksFactory(NewHook, address)
	t.Cleanup(func() {
		if existed {
			v4.HookFactories[address] = old
		} else {
			delete(v4.HookFactories, address)
		}
	})
	static := v4.StaticExtra{Fee: m.Fee, TickSpacing: int32(m.TickSpacing), HooksAddress: address}
	rawStatic, err := json.Marshal(static)
	require.NoError(t, err)
	ep := entity.Pool{Address: strings.ToLower(m.PoolID), Exchange: string(Exchange), Type: v4.DexType, StaticExtra: string(rawStatic), BlockNumber: m.Block, Tokens: []*entity.PoolToken{{Address: strings.ToLower(m.Token0), Swappable: true}, {Address: strings.ToLower(m.Token1), Swappable: true}}}
	block := new(big.Int).SetUint64(m.Block)
	param := &v4.HookParam{HookAddress: address, BlockNumber: block, RpcClient: client, Pool: &ep}
	rawTerms, err := NewHook(param).Track(context.Background(), param)
	require.NoError(t, err)
	var terms Extra
	require.NoError(t, json.Unmarshal(rawTerms, &terms))
	require.True(t, terms.Registered)
	a, err := abi.JSON(strings.NewReader(bridgeStateABI))
	require.NoError(t, err)
	var slot struct{ SqrtPriceX96, Tick, ProtocolFee, LpFee *big.Int }
	var liquidity *big.Int
	var ticks [2]struct{ LiquidityGross, LiquidityNet, FeeGrowthOutside0X128, FeeGrowthOutside1X128 *big.Int }
	require.Positive(t, m.TickSpacing)
	lower, upper := (-887272/m.TickSpacing)*m.TickSpacing, (887272/m.TickSpacing)*m.TickSpacing
	id := common.HexToHash(ep.Address)
	request := client.NewRequest().SetContext(context.Background()).SetBlockNumber(block)
	request.AddCall(&ethrpc.Call{ABI: a, Target: bridgeStateView, Method: "getSlot0", Params: []any{id}}, []any{&slot})
	request.AddCall(&ethrpc.Call{ABI: a, Target: bridgeStateView, Method: "getLiquidity", Params: []any{id}}, []any{&liquidity})
	for i, tick := range []int{lower, upper} {
		request.AddCall(&ethrpc.Call{ABI: a, Target: bridgeStateView, Method: "getTickInfo", Params: []any{id, big.NewInt(int64(tick))}}, []any{&ticks[i]})
	}
	_, err = request.Aggregate()
	require.NoError(t, err)
	require.Equal(t, liquidity.String(), ticks[0].LiquidityGross.String(), "one full-range position")
	require.Equal(t, liquidity.String(), ticks[1].LiquidityGross.String())
	protocol, lp := slot.ProtocolFee.Uint64()&4095, slot.LpFee.Uint64()
	ep.SwapFee = float64(protocol + lp - protocol*lp/1_000_000)
	fetch := v4.FetchRPCResult{Liquidity: liquidity, TickSpacing: int32(m.TickSpacing), Slot0: v4.Slot0Data{SqrtPriceX96: slot.SqrtPriceX96, Tick: slot.Tick, LpFee: uint32(lp), ProtocolFee: uint32(slot.ProtocolFee.Uint64())}, HookExtra: rawTerms}
	extra := fetch.ToExtra([]v4.Tick{{Index: lower, LiquidityGross: ticks[0].LiquidityGross, LiquidityNet: ticks[0].LiquidityNet}, {Index: upper, LiquidityGross: ticks[1].LiquidityGross, LiquidityNet: ticks[1].LiquidityNet}})
	raw, err := json.Marshal(extra)
	require.NoError(t, err)
	ep.Extra = string(raw)
	r0, r1 := v4.EstimateReservesFromTicks(slot.SqrtPriceX96, extra.Ticks)
	ep.Reserves = entity.PoolReserves{r0.String(), r1.String()}
	sim, err := v4.NewPoolSimulator(ep, valueobject.ChainIDMonad)
	require.NoError(t, err)
	static.HooksAddress = common.Address{}
	rawStatic, err = json.Marshal(static)
	require.NoError(t, err)
	ep.StaticExtra = string(rawStatic)
	plain, err := v4.NewPoolSimulator(ep, valueobject.ChainIDMonad)
	require.NoError(t, err)
	return sim, plain, terms
}
