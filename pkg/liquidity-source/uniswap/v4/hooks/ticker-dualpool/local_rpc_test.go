package tickerdualpool

import (
	"context"
	"math/big"
	"net"
	"net/url"
	"os"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/goccy/go-json"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
)

func TestTrackerAgainstFreshLocalMonadFork(t *testing.T) {
	endpoint := os.Getenv("KYBER_LOCAL_RPC")
	if endpoint == "" {
		t.Skip("requires the exported local Monad fixture")
	}
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.True(t, net.ParseIP(u.Hostname()).IsLoopback(), "this test only accepts local RPC")
	raw, err := os.ReadFile("/tmp/ticker-kyber-rpc-manifest.json")
	require.NoError(t, err)
	var manifest struct{ Hook, Vault, PoolId, StateView, Token0, Token1 string }
	require.NoError(t, json.Unmarshal(raw, &manifest))
	address := common.HexToAddress(manifest.Hook)
	extra, err := json.Marshal(uniswapv4.StaticExtra{Fee: 2000, TickSpacing: 10, HooksAddress: address})
	require.NoError(t, err)
	client := ethrpc.New(endpoint).SetMulticallContract(common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))
	p := &uniswapv4.HookParam{HookAddress: address, BlockNumber: big.NewInt(111021450), RpcClient: client,
		Cfg:  &uniswapv4.Config{StateViewAddress: manifest.StateView},
		Pool: &entity.Pool{Address: manifest.PoolId, StaticExtra: string(extra), Tokens: []*entity.PoolToken{{Address: manifest.Token0}, {Address: manifest.Token1}}}}
	implSlot := common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")
	implRaw, err := client.GetETHClient().StorageAt(context.Background(), address, implSlot, p.BlockNumber)
	require.NoError(t, err)
	implementation := common.BytesToAddress(implRaw)
	code, err := client.GetETHClient().CodeAt(context.Background(), implementation, p.BlockNumber)
	require.NoError(t, err)
	require.True(t, tickerHookFingerprint.matches(code), "fork hook must match the pinned accounting implementation")
	code, err = client.GetETHClient().CodeAt(context.Background(), common.HexToAddress(manifest.Vault), p.BlockNumber)
	require.NoError(t, err)
	require.True(t, tickerVaultFingerprint.matches(code), "fork wrapper must match the pinned TickerVault implementation")
	h := NewHook(p).(*Hook)
	_, err = h.Track(context.Background(), p)
	require.NoError(t, err)
	require.Equal(t, "9999999999", h.Total0.Dec())
	require.Equal(t, "1000000000000000000000000", h.Total1.Dec())
	require.Equal(t, common.HexToAddress(manifest.Vault), h.Vaults[0])
	q, err := h.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: true, AmountSpecified: big.NewInt(100000000)})
	require.NoError(t, err)
	require.Equal(t, "-9793771323500773836399", q.DeltaUnspecified.String())
	// Full tracker -> quote -> state update -> reverse quote, with no injected
	// intermediate contract snapshot. Both outputs come from the fork fixture.
	sequential := h.CloneState().(*Hook)
	sequential.UpdateBalance(q.SwapInfo)
	sell := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	reverse, err := sequential.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: false, AmountSpecified: sell})
	require.NoError(t, err)
	require.Equal(t, "-9901813", reverse.DeltaUnspecified.String())
	managerFixture, err := os.ReadFile("testdata/manager-cash.json")
	require.NoError(t, err)
	var managerStates []struct{ Before, After []string }
	require.NoError(t, json.Unmarshal(managerFixture, &managerStates))
	for side := 0; side < 2; side++ {
		require.Equal(t, managerStates[0].Before[side], h.ManagerCash[side].Dec())
		require.Equal(t, managerStates[0].After[side], sequential.ManagerCash[side].Dec(), "physical cash after first settled swap")
		require.Equal(t, managerStates[1].After[side], reverse.SwapInfo.(Quote).ManagerCashAfter[side].Dec(), "physical cash after reverse swap")
	}
	for _, paused := range []bool{false, true} {
		unavailable := h.CloneState().(*Hook)
		if paused {
			unavailable.Reserves[0].Paused = true
		} else {
			unavailable.Reserves[0].Cash.Clear()
		}
		_, err := unavailable.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: true, AmountSpecified: big.NewInt(100000000)})
		require.ErrorIs(t, err, ErrVaultIlliquid)
	}
	frozen := h.CloneState().(*Hook)
	frozen.Reserves[0].Frozen = true
	frozenBuy, err := frozen.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: true, AmountSpecified: big.NewInt(100000000)})
	require.NoError(t, err)
	frozen.UpdateBalance(frozenBuy.SwapInfo)
	require.False(t, frozen.Aave[0].Raw.IsZero(), "failed redeposit retains raw pool cash")
	_, err = frozen.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: false, AmountSpecified: sell})
	require.NoError(t, err)
	// Selling the freshly created test token creates claims before the router
	// has paid that token to PoolManager. A second leg must see that shortage.
	deferred := h.CloneState().(*Hook)
	firstSell, err := deferred.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: false, AmountSpecified: sell})
	require.NoError(t, err)
	deferred.UpdateBalanceWithoutSettlement(firstSell.SwapInfo)
	require.False(t, deferred.Unbacked1.IsZero())
	_, err = deferred.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: false, AmountSpecified: sell})
	require.ErrorIs(t, err, ErrVaultIlliquid)
	paid := h.CloneState().(*Hook)
	paid.UpdateBalance(firstSell.SwapInfo)
	require.True(t, paid.Unbacked1.IsZero())
	_, err = paid.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: false, AmountSpecified: sell})
	require.NoError(t, err)
	testUnbackedRawSide(t, h, p)
}

// Insert an unsettled claim on the raw-token side without changing the node.
// The override is used by both eth_call and our explicit storage reads.
func testUnbackedRawSide(t *testing.T, h *Hook, p *uniswapv4.HookParam) {
	t.Helper()
	currency := common.HexToAddress(p.Pool.Tokens[1].Address)
	id := common.HexToHash(p.Pool.Address)
	partition := crypto.Keccak256Hash(id[:], common.LeftPadBytes(currency.Bytes(), 32))
	index := common.BigToHash(big.NewInt(5))
	slot := crypto.Keccak256Hash(partition[:], index[:])
	packed := new(uint256.Int).Lsh(uint256.NewInt(1), 128)
	packed.Or(packed, h.Raw[1])
	p.Overrides = map[common.Address]gethclient.OverrideAccount{
		h.address: {StateDiff: map[common.Hash]common.Hash{slot: common.BigToHash(packed.ToBig())}},
		// balanceOf(PoolManager) reports zero; no actual token or pool state is changed.
		currency: {Code: common.FromHex("0x600060005260206000f3")},
	}
	clone := h.CloneState().(*Hook)
	_, err := clone.Track(context.Background(), p)
	require.NoError(t, err)
	require.Equal(t, "1", clone.Unbacked1.Dec())
	_, err = clone.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, ZeroForOne: true, AmountSpecified: big.NewInt(100000000)})
	require.ErrorIs(t, err, ErrVaultIlliquid)
	require.True(t, h.Unbacked1.IsZero(), "tracking a clone must not change the original")
}
