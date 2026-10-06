package tickerdualpool

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestAaveReserveCashShareCashRounding(t *testing.T) {
	r := AaveReserve{Cash: uint256.NewInt(1000), ScaledSupply: uint256.NewInt(10000), SupplyCap: uint256.NewInt(0), Active: true}
	rate := new(uint256.Int).Add(ray, uint256.NewInt(1))
	limit, err := r.withdrawalLimit(uint256.NewInt(2000), rate)
	require.NoError(t, err)
	require.Equal(t, "999", limit.Dec())
	require.ErrorIs(t, r.withdraw(uint256.NewInt(1000), uint256.NewInt(2000), rate), ErrVaultIlliquid)
	require.Equal(t, "1000", r.Cash.Dec(), "failed withdrawal must not consume cash")
}

func TestAaveReserveWithdrawalFreesCapButDoesNotUnfreeze(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		r := AaveReserve{Cash: uint256.NewInt(1000), ScaledSupply: uint256.NewInt(1000), SupplyCap: uint256.NewInt(1000), Active: true, Frozen: frozen}
		limit, err := r.depositLimit(ray)
		require.NoError(t, err)
		require.True(t, limit.IsZero())
		require.NoError(t, r.withdraw(uint256.NewInt(400), uint256.NewInt(1000), ray))
		accepted, err := r.deposit(uint256.NewInt(400), ray)
		require.NoError(t, err)
		require.Equal(t, !frozen, accepted)
		if frozen {
			require.Equal(t, "600", r.Cash.Dec())
		} else {
			require.Equal(t, "1000", r.Cash.Dec())
		}
	}
}

func TestAaveFrozenRedepositKeepsPoolCash(t *testing.T) {
	v := AaveInventory{Raw: uint256.NewInt(0), Claims: uint256.NewInt(0), PoolShares: uint256.NewInt(1000000000), Supply: uint256.NewInt(1000000000), Idle: uint256.NewInt(0), StrategyShares: uint256.NewInt(1000), Rate: ray.Clone(), PendingYield: uint256.NewInt(0)}
	reserve := AaveReserve{Cash: uint256.NewInt(1000), ScaledSupply: uint256.NewInt(1000), SupplyCap: uint256.NewInt(0), Active: true, Frozen: true}
	next, r, err := v.AfterJITWithReserve(uint256.NewInt(400), uint256.NewInt(300), reserve)
	require.NoError(t, err)
	require.Equal(t, "300", next.Raw.Dec(), "withdraw 400, spend net 100, keep 300 when deposit fails")
	require.Equal(t, "600", next.StrategyShares.Dec())
	require.Equal(t, "600", r.Cash.Dec())
	require.Equal(t, "1000", reserve.Cash.Dec(), "quote must not mutate input reserve")
	require.Equal(t, "1000", v.StrategyShares.Dec())
	reserve.Frozen = false
	next, r, err = v.AfterJITWithReserve(uint256.NewInt(400), uint256.NewInt(300), reserve)
	require.NoError(t, err)
	require.True(t, next.Raw.IsZero())
	require.Equal(t, "900", next.StrategyShares.Dec())
	require.Equal(t, "900", r.Cash.Dec())
}

func TestAavePausedWithdrawalRejected(t *testing.T) {
	r := AaveReserve{Cash: uint256.NewInt(1000), ScaledSupply: uint256.NewInt(1000), SupplyCap: uint256.NewInt(0), Active: true, Paused: true}
	require.ErrorIs(t, r.withdraw(uint256.NewInt(1), uint256.NewInt(1000), ray), ErrVaultIlliquid)
	accepted, err := r.deposit(uint256.NewInt(1), ray)
	require.NoError(t, err)
	require.False(t, accepted)
}

func TestTreasuryAccrualCanExhaustDepositCap(t *testing.T) {
	r := AaveReserve{Cash: uint256.NewInt(990), ScaledSupply: uint256.NewInt(990), SupplyCap: uint256.NewInt(1000), PendingTreasury: uint256.NewInt(10), Active: true}
	limit, err := r.depositLimit(ray)
	require.NoError(t, err)
	require.Equal(t, "10", limit.Dec(), "maxDeposit sees stored treasury before the reserve update")
	accepted, err := r.deposit(uint256.NewInt(10), ray)
	require.NoError(t, err)
	require.False(t, accepted, "pool validation sees newly accrued treasury and rejects the deposit")
	require.Equal(t, "990", r.ScaledSupply.Dec(), "reverted deposit must not accrue or mint")
	require.Equal(t, "10", r.PendingTreasury.Dec())
	require.NoError(t, r.withdraw(uint256.NewInt(20), uint256.NewInt(990), ray))
	require.Equal(t, "980", r.ScaledSupply.Dec(), "withdrawal burns 20 and accrues 10")
	require.True(t, r.PendingTreasury.IsZero())
	limit, err = r.depositLimit(ray)
	require.NoError(t, err)
	require.Equal(t, "20", limit.Dec())
}
