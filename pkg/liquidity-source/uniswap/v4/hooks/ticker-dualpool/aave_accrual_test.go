package tickerdualpool

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestElapsedTreasuryAccrualMatchesMonadFork(t *testing.T) {
	raw, err := os.ReadFile("testdata/aave-accrual.json")
	require.NoError(t, err)
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
