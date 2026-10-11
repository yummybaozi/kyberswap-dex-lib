package tickerdualpool

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/dualpool"
)

func TestGasEstimateCoversMeasuredFixtureCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/gas.json")
	require.NoError(t, err)
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
