package tickerdualpool

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestElapsedTreasuryAccrualMatchesMonadFork(t *testing.T) {
	raw := []byte(capturedAaveAccrual)
	var f map[string]string
	require.NoError(t, json.Unmarshal(raw, &f))
	n := func(key string) *uint256.Int { return uint256.MustFromDecimal(f[key]) }
	factor, err := strconv.ParseUint(f["factor"], 10, 64)
	require.NoError(t, err)
	pending, err := pendingTreasuryShares(n("scaledDebt"), n("previousIndex"), n("nextIndex"), n("rate"), factor)
	require.NoError(t, err)
	require.Equal(t, f["treasuryDelta"], pending.Dec())
	reserve := AaveReserve{Cash: n("cashBefore"), ScaledSupply: n("supplyBefore"), SupplyCap: new(uint256.Int), PendingTreasury: pending, Active: true}
	require.NoError(t, reserve.withdraw(n("withdraw"), n("held"), n("rate")))
	require.Equal(t, f["supplyAfter"], reserve.ScaledSupply.Dec())
	require.Equal(t, f["cashAfter"], reserve.Cash.Dec())
	require.True(t, reserve.PendingTreasury.IsZero())
}

// Captured contract results; preserved from aave-accrual.json.
const capturedAaveAccrual = `{
"rate":"1010239873045829464919591085",
"factor":"1000",
"scaledDebt":"156522557133680",
"previousIndex":"1012526855659549145199017959",
"nextIndex":"1012664406964246121576712621",
"supplyBefore":"171976086350974",
"cashBefore":"15234432117736",
"held":"9899743234",
"withdraw":"100000000",
"treasuryDelta":"2131165332",
"supplyAfter":"171978118529914",
"cashAfter":"15234332117736"
}`
