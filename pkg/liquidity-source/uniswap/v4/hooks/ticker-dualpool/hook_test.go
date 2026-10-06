package tickerdualpool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

func localSimulator(t *testing.T) *uniswapv4.PoolSimulator {
	t.Helper()
	address := common.HexToAddress("0x0000000000000000000000000000000000012ac4")
	previous, exists := uniswapv4.HookFactories[address]
	uniswapv4.RegisterHooksFactory(NewHook, address)
	t.Cleanup(func() {
		if exists {
			uniswapv4.HookFactories[address] = previous
		} else {
			delete(uniswapv4.HookFactories, address)
		}
	})
	state := fixtureState()
	snapshot, err := json.Marshal(Snapshot{State: state, BlockNumber: 1})
	require.NoError(t, err)
	fetch := uniswapv4.FetchRPCResult{Liquidity: big.NewInt(0), TickSpacing: 10,
		Slot0: uniswapv4.Slot0Data{SqrtPriceX96: state.Pool.SqrtPriceX96.ToBig(), Tick: big.NewInt(0), LpFee: 2000}, HookExtra: snapshot}
	extra, err := json.Marshal(fetch.ToExtra([]uniswapv4.Tick{}))
	require.NoError(t, err)
	static, err := json.Marshal(uniswapv4.StaticExtra{Fee: 2000, TickSpacing: 10, HooksAddress: address})
	require.NoError(t, err)
	p := entity.Pool{Address: "0x0000000000000000000000000000000000000000000000000000000000000001", Type: uniswapv4.DexType,
		Tokens:   []*entity.PoolToken{{Address: "0x0000000000000000000000000000000000000001", Decimals: 18, Swappable: true}, {Address: "0x0000000000000000000000000000000000000002", Decimals: 18, Swappable: true}},
		Reserves: entity.PoolReserves{state.Pool.Balance0.Dec(), state.Pool.Balance1.Dec()}, Extra: string(extra), StaticExtra: string(static), BlockNumber: 1}
	sim, err := uniswapv4.NewPoolSimulator(p, valueobject.ChainIDMonad)
	require.NoError(t, err)
	return sim
}

func TestKyberSimulatorAppliesTickerFees(t *testing.T) {
	sim := localSimulator(t)
	input := pool.TokenAmount{Token: sim.GetTokens()[0], Amount: big.NewInt(1001)}
	result, err := sim.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: input, TokenOut: sim.GetTokens()[1]})
	require.NoError(t, err)
	require.Equal(t, "984", result.TokenAmountOut.Amount.String())
	clone := sim.CloneState()
	sim.UpdateBalance(pool.UpdateBalanceParams{TokenAmountIn: input, TokenAmountOut: *result.TokenAmountOut, SwapInfo: result.SwapInfo})
	// An independent clone must still give the original quote.
	again, err := clone.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: input, TokenOut: sim.GetTokens()[1]})
	require.NoError(t, err)
	require.Equal(t, result.TokenAmountOut.Amount.String(), again.TokenAmountOut.Amount.String())
	reverse, err := sim.CalcAmountIn(pool.CalcAmountInParams{TokenAmountOut: pool.TokenAmount{Token: sim.GetTokens()[1], Amount: big.NewInt(198)}, TokenIn: sim.GetTokens()[0]})
	require.NoError(t, err)
	check, err := sim.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: *reverse.TokenAmountIn, TokenOut: sim.GetTokens()[1]})
	require.NoError(t, err)
	require.GreaterOrEqual(t, check.TokenAmountOut.Amount.Int64(), int64(198))
}

func TestHookCloneAndVaultRefresh(t *testing.T) {
	h := &Hook{Snapshot: Snapshot{State: fixtureState()}}
	h.Vaults[1] = common.HexToAddress("0x1234")
	q, err := QuoteExactInput(h.State, true, uint256.NewInt(1001))
	require.NoError(t, err)
	clone := h.CloneState().(*Hook)
	clone.State.Pool.Buckets[0].WeightBps = 1
	require.Equal(t, uint16(10000), h.State.Pool.Buckets[0].WeightBps)
	h.UpdateBalance(q)
	_, err = h.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: true, AmountSpecified: big.NewInt(1001)})
	require.ErrorIs(t, err, ErrRefreshRequired)
	require.False(t, clone.refreshRequired)
	q.Swap.Balance0.Clear()
	require.False(t, h.State.Pool.Balance0.IsZero(), "UpdateBalance must not retain mutable quote pointers")
}
