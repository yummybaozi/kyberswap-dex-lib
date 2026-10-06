package dualpool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"

	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	ponsv2 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/pons-v2"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

// Audit-only probes. These do not implement Ticker support or claim an on-chain comparison.
func TestTickerAudit_UnregisteredHookIsRejected(t *testing.T) {
	p := entityPool
	var extra uniswapv4.StaticExtra
	require.NoError(t, json.Unmarshal([]byte(p.StaticExtra), &extra))
	extra.HooksAddress = common.HexToAddress("0x0000000000000000000000000000000000002ac4")
	raw, err := json.Marshal(extra)
	require.NoError(t, err)
	p.StaticExtra = string(raw)
	_, err = uniswapv4.NewPoolSimulator(p, valueobject.ChainIDMonad)
	require.Error(t, err)
	t.Logf("unregistered hook: %v", err)
}

type naiveTickerCombination struct {
	*Hook
	seenOutput *big.Int
}

func (h *naiveTickerCombination) CanAfterSwap(common.Address) bool { return true }
func (h *naiveTickerCombination) AfterSwap(p *uniswapv4.AfterSwapParams) (*uniswapv4.AfterSwapResult, error) {
	h.seenOutput = new(big.Int).Set(p.AmountOut)
	fee := ponsv2.Extra{Registered: true, FeeBps: 100}
	return fee.AfterSwap(p)
}
func TestTickerAudit_NaiveDualPoolPlusPonsFeeChargesZero(t *testing.T) {
	p := entityPool
	var extra uniswapv4.StaticExtra
	require.NoError(t, json.Unmarshal([]byte(p.StaticExtra), &extra))
	address := common.HexToAddress("0x0000000000000000000000000000000000012ac4")
	var hook *naiveTickerCombination
	uniswapv4.RegisterHooksFactory(func(param *uniswapv4.HookParam) uniswapv4.Hook {
		base := &Hook{Hook: &uniswapv4.BaseHook{}, hook: address}
		require.NoError(t, param.HookExtra.Unmarshal(&base.state))
		hook = &naiveTickerCombination{Hook: base}
		return hook
	}, address)
	t.Cleanup(func() { delete(uniswapv4.HookFactories, address) })
	extra.HooksAddress = address
	raw, err := json.Marshal(extra)
	require.NoError(t, err)
	p.StaticExtra = string(raw)
	sim, err := uniswapv4.NewPoolSimulator(p, valueobject.ChainIDMonad)
	require.NoError(t, err)
	result, err := sim.CalcAmountOut(pool.CalcAmountOutParams{
		TokenAmountIn: pool.TokenAmount{Token: p.Tokens[0].Address, Amount: big.NewInt(100000000)},
		TokenOut:      p.Tokens[1].Address,
	})
	require.NoError(t, err)
	require.Zero(t, hook.seenOutput.Sign(), "ordinary AfterSwap AmountOut excludes output supplied by DualPool BeforeSwap")
	gross := big.NewInt(421126678297991910)
	require.Equal(t, gross.String(), result.TokenAmountOut.Amount.String())
	expectedNet := new(big.Int).Sub(gross, new(big.Int).Div(new(big.Int).Set(gross), big.NewInt(100)))
	require.NotEqual(t, expectedNet.String(), result.TokenAmountOut.Amount.String())
	t.Logf("gross=%s; naive fee-model output=%s; correct 1%%-fee arithmetic=%s; AfterSwap saw=%s", gross, result.TokenAmountOut.Amount, expectedNet, hook.seenOutput)
}
