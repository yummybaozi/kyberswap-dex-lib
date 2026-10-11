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

func TestDirectionalProtocolFeesMatchMonadFork(t *testing.T) {
	raw, err := os.ReadFile("testdata/protocol-fees.json")
	require.NoError(t, err)
	var rows []map[string]string
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.Len(t, rows, 4)
	for _, f := range rows {
		t.Run(f["direction"]+"/"+f["input"], func(t *testing.T) {
			s := fixtureState()
			s.Pool.Balance0 = uint256.MustFromDecimal(f["before0"])
			s.Total0 = s.Pool.Balance0.Clone()
			s.Pool.Balance1 = uint256.MustFromDecimal(f["before1"])
			s.Total1 = s.Pool.Balance1.Clone()
			s.Pool.SqrtPriceX96 = uint256.MustFromDecimal(f["initialPrice"])
			tick, err := strconv.Atoi(f["initialTick"])
			require.NoError(t, err)
			s.Pool.Tick = int32(tick)
			protocol, err := strconv.ParseUint(f["protocol"], 10, 32)
			require.NoError(t, err)
			s.Pool.ProtocolFee = uint32(protocol)
			s.Pool.Buckets = []dualpool.Bucket{{TickLower: 312380, TickUpper: 332380, WeightBps: 10000}}
			q, err := QuoteExactInput(s, f["direction"] == "true", uint256.MustFromDecimal(f["input"]))
			require.NoError(t, err)
			require.Equal(t, f["net"], q.NetOutput.Dec())
			require.Equal(t, f["fee"], q.HookFee.Dec())
			require.Equal(t, f["tax"], q.CreatorTax.Dec())
			require.Equal(t, f["price"], q.Swap.SqrtPriceX96.Dec())
			require.Equal(t, f["tick"], strconv.Itoa(int(q.Swap.Tick)))
		})
	}
}
