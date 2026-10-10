package tickermemehook_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	tickermemehook "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/ticker-memehook"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/msgpack"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

// This file is an external test package because pkg/msgpack imports this package
// to register its Hook type (register_pool_types.gen.go).

var testHook = common.HexToAddress("0x7e570000000000000000000000000000000000c1")

// TestMsgpackRoundTrip_PoolSimulator runs the simulator through the encoding the
// router uses to store and load pool simulators and checks it quotes the same.
func TestMsgpackRoundTrip_PoolSimulator(t *testing.T) {
	tickermemehook.RegisterHookAddresses(testHook)
	p := entity.Pool{
		Address: "0x0f8e0a2a1a4c6a8f2b3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293", Exchange: string(tickermemehook.Exchange),
		Type: "uniswap-v4", SwapFee: 3000, Reserves: entity.PoolReserves{"1000000000000000000000000", "1000000000000000000000000"},
		Tokens: []*entity.PoolToken{{Address: "0x1000000000000000000000000000000000000001", Decimals: 18, Swappable: true},
			{Address: "0x2000000000000000000000000000000000000002", Decimals: 6, Swappable: true}},
		Extra: `{"liquidity":1000000000000000000,"tickSpacing":60,"tick":0,"sqrtPriceX96":79228162514264337593543950336,` +
			`"ticks":[{"index":-887220,"liquidityGross":1000000000000000000,"liquidityNet":1000000000000000000},` +
			`{"index":887220,"liquidityGross":1000000000000000000,"liquidityNet":-1000000000000000000}],` +
			`"hX":{"reg":true,"hF":100,"cT":133}}`,
		StaticExtra: `{"0x0":[false,false],"fee":3000,"tS":60,"hooks":"` + testHook.Hex() + `"}`,
	}
	sim, err := uniswapv4.NewPoolSimulator(p, valueobject.ChainIDMonad)
	require.NoError(t, err)
	encoded, err := msgpack.EncodePoolSimulatorsMap(map[string]pool.IPoolSimulator{p.Address: sim})
	require.NoError(t, err)
	decoded, err := msgpack.DecodePoolSimulatorsMap(encoded)
	require.NoError(t, err)
	restored, ok := decoded[p.Address].(*uniswapv4.PoolSimulator)
	require.True(t, ok, "decoded simulator is %T", decoded[p.Address])

	for _, zeroForOne := range []bool{true, false} {
		in, out := p.Tokens[0].Address, p.Tokens[1].Address
		if !zeroForOne {
			in, out = out, in
		}
		amount := big.NewInt(123_456_789)
		want, err := sim.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: in, Amount: amount}, TokenOut: out})
		require.NoError(t, err)
		got, err := restored.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: in, Amount: amount}, TokenOut: out})
		require.NoError(t, err)
		require.Equal(t, want.TokenAmountOut.Amount.String(), got.TokenAmountOut.Amount.String())
		require.Equal(t, "120218504", want.TokenAmountOut.Amount.String()) // 123456789 less 30 bps LP fee, less 1% + 1.33% skim
		wantIn, err := sim.CalcAmountIn(pool.CalcAmountInParams{TokenAmountOut: pool.TokenAmount{Token: out, Amount: amount}, TokenIn: in})
		require.NoError(t, err)
		gotIn, err := restored.CalcAmountIn(pool.CalcAmountInParams{TokenAmountOut: pool.TokenAmount{Token: out, Amount: amount}, TokenIn: in})
		require.NoError(t, err)
		require.Equal(t, wantIn.TokenAmountIn.Amount.String(), gotIn.TokenAmountIn.Amount.String())
	}
}
