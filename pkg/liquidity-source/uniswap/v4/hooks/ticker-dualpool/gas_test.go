package tickerdualpool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/dualpool"
)

func TestGasEstimateCoversMeasuredFixtureCases(t *testing.T) {
	raw := []byte(capturedGas)
	var fixtures map[string]struct {
		Measurements []struct{ EstimatedGas int64 }
	}
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	for name, fixture := range fixtures {
		count := 1
		if name == "eightBuckets" {
			count = 8
		}
		h := &Hook{Snapshot: Snapshot{State: fixtureState()}}
		h.Pool.Buckets = make([]dualpool.Bucket, count)
		q := Quote{Swap: dualpool.SwapInfo{Tick: h.Pool.Tick}}
		estimate := h.quoteGas(q) + uniswapv4.DefaultGas.BaseGas
		for _, m := range fixture.Measurements {
			require.GreaterOrEqual(t, estimate, m.EstimatedGas)
		}
	}
}

// Captured contract results; preserved from gas.json.
const capturedGas = `{
"oneBucket":{"block":111021450,"router":"0x03A6a84cD762D9707A21605b548aaaB891562aAb","measurements":[{"zeroForOne":true,"input":"100000000","estimatedGas":1377096,"returnData":"0xfffffffffffffffffffffffffa0a1f000000000000000212ebe159197eed066f"},{"zeroForOne":false,"input":"1000000000000000000000","estimatedGas":1392376,"returnData":"0x0000000000000000000000000095e8fbffffffffffffffc9ca36523a21600000"},{"zeroForOne":true,"input":"1000000000","estimatedGas":1376968,"returnData":"0xffffffffffffffffffffffffc46536000000000000001408a42030855558fcd2"},{"zeroForOne":false,"input":"100000000000000000000000","estimatedGas":1399609,"returnData":"0x000000000000000000000000385e43d0ffffffffffffead2fd381eb509800000"}]},
"eightBuckets":{"block":111021450,"router":"0x03A6a84cD762D9707A21605b548aaaB891562aAb","measurements":[{"zeroForOne":true,"input":"100000000","estimatedGas":2428452,"returnData":"0xfffffffffffffffffffffffffa0a1f000000000000000212ea73080841ba0d55"},{"zeroForOne":false,"input":"1000000000000000000000","estimatedGas":2433634,"returnData":"0x0000000000000000000000000095e8f1ffffffffffffffc9ca36523a21600000"},{"zeroForOne":true,"input":"1000000000","estimatedGas":2428318,"returnData":"0xffffffffffffffffffffffffc465360000000000000014081e9d0a9ab81360f0"},{"zeroForOne":false,"input":"100000000000000000000000","estimatedGas":2439949,"returnData":"0x000000000000000000000000385ccc39ffffffffffffead2fd381eb509800000"}]}
}`
