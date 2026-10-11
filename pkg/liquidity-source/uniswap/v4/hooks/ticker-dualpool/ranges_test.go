package tickerdualpool

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4/hooks/dualpool"
)

func TestRangeConfigurationsMatchSolidity(t *testing.T) {
	raw, err := os.ReadFile("testdata/range-quotes.json")
	require.NoError(t, err)
	var rows []map[string]string
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.Len(t, rows, 18)
	for _, row := range rows {
		t.Run(row["config"]+"/"+row["side"]+"/"+row["input"], func(t *testing.T) {
			s := fixtureState()
			config, err := strconv.Atoi(row["config"])
			require.NoError(t, err)
			s.Pool.Buckets = rangeBuckets(config)
			q, err := QuoteExactInput(s, row["side"] == "0", uint256.MustFromDecimal(row["input"]))
			require.NoError(t, err)
			require.Equal(t, row["used"], q.Swap.AmountIn.Dec())
			require.Equal(t, row["net"], q.NetOutput.Dec())
			require.Equal(t, row["fee"], q.HookFee.Dec())
			require.Equal(t, row["tax"], q.CreatorTax.Dec())
			require.Equal(t, row["price"], q.Swap.SqrtPriceX96.Dec())
			require.Equal(t, row["tick"], strconv.Itoa(int(q.Swap.Tick)))
			require.Equal(t, row["after0"], q.Swap.Balance0.Dec())
			require.Equal(t, row["after1"], q.Swap.Balance1.Dec())
		})
	}
}

func TestExhaustedRangesStopAtPriceLimit(t *testing.T) {
	for _, direction := range []bool{true, false} {
		s := fixtureState()
		s.Pool.Buckets = rangeBuckets(2)
		_, err := QuoteExactInput(s, direction, uint256.MustFromDecimal("1000000000000000000000000000000"))
		require.ErrorIs(t, err, dualpool.ErrInsufficientLiquidity)
	}
}
