package msgpack

import (
	"math/big"
	"testing"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	v4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	_ "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/dualpool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

// Twofold's hook embeds Hook as an interface containing *BaseHook. Before that
// concrete type was registered, decoding this simulator panicked.
func TestTwofoldSerializationPreservesQuotes(t *testing.T) {
	var p entity.Pool
	require.NoError(t, json.Unmarshal([]byte(`{"address":"0x085e812d2b072f9569c192769fef074eac9b3b519647665c98a7e4119f8fa06b","exchange":"uniswap-v4-dualpool","type":"uniswap-v4","timestamp":1788618000,"reserves":["263339524","1140173744005663979"],"tokens":[{"address":"0x5fc5360d0400a0fd4f2af552add042d716f1d168","symbol":"USDG","decimals":6,"swappable":true},{"address":"0xd0601ce157db5bdc3162bbac2a2c8af5320d9eec","symbol":"NVDA","decimals":18,"swappable":true}],"extra":"{\"liquidity\":0,\"sqrtPriceX96\":5214115250030972493292480690016563,\"tickSpacing\":10,\"tick\":221902,\"ticks\":[],\"hX\":{\"live\":true,\"b0\":\"0xfb23e04\",\"b1\":\"0xfd2b5fab1af4ceb\",\"sP\":\"0x1011363067154f9d3faa462565133\",\"t\":221902,\"pF\":0,\"lF\":500,\"bk\":[{\"l\":220700,\"u\":223100,\"w\":7000},{\"l\":217700,\"u\":226100,\"w\":3000}]}}","staticExtra":"{\"0x0\":[false,false],\"fee\":500,\"tS\":10,\"hooks\":\"0xd1bcbcca41f3bdb6b4812652959c6df725ea2ac0\",\"uR\":\"0x8876789976decbfcbbbe364623c63652db8c0904\",\"pm2\":\"0x000000000022d473030f116ddee9f6b43ac78ba3\",\"mc3\":\"0xca11bde05977b3631167028862be2a173976ca11\"}","blockNumber":55199380}`), &p))
	original, err := v4.NewPoolSimulator(p, valueobject.ChainIDRobinhood)
	require.NoError(t, err)
	encoded, err := EncodePoolSimulatorsMap(map[string]pool.IPoolSimulator{p.Address: original})
	require.NoError(t, err)
	decoded, err := DecodePoolSimulatorsMap(encoded)
	require.NoError(t, err)
	restored := decoded[p.Address]
	params := pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: p.Tokens[0].Address, Amount: big.NewInt(100_000_000)}, TokenOut: p.Tokens[1].Address}
	for i := 0; i < 2; i++ {
		want, err := original.CalcAmountOut(params)
		require.NoError(t, err)
		got, err := restored.CalcAmountOut(params)
		require.NoError(t, err)
		require.Equal(t, want.TokenAmountOut.Amount.String(), got.TokenAmountOut.Amount.String())
		if i == 0 {
			require.Equal(t, "421126678297991910", got.TokenAmountOut.Amount.String())
		}
		original.UpdateBalance(pool.UpdateBalanceParams{TokenAmountIn: params.TokenAmountIn, TokenAmountOut: *want.TokenAmountOut, SwapInfo: want.SwapInfo})
		restored.UpdateBalance(pool.UpdateBalanceParams{TokenAmountIn: params.TokenAmountIn, TokenAmountOut: *got.TokenAmountOut, SwapInfo: got.SwapInfo})
	}
}
