package tickerdualpool

import (
	"context"
	"encoding/json"
	"math/big"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	v4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/swaplimit"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

type BridgePool struct {
	Hook        string `json:"hook"`
	Token0      string `json:"token0"`
	Token1      string `json:"token1"`
	PoolId      string `json:"poolId"`
	Fee         uint32 `json:"fee"`
	TickSpacing int    `json:"tickSpacing"`
}

// TestExecutorBridge exports a quote from the real tracker and parent V4 simulator.
// The Solidity harness consumes it before executing any reference swap.
func TestExecutorBridge(t *testing.T) {
	path := os.Getenv("KYBER_BRIDGE_REQUEST")
	if path == "" {
		t.Skip("requires local executor harness")
	}
	endpoint := os.Getenv("KYBER_LOCAL_RPC")
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.True(t, u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback(), "local RPC only")
	var m struct {
		BridgePool
		TokenIn, AmountIn string
		RoundTrip         bool
		Next              json.RawMessage
		Block, Timestamp  uint64
		AaveCash          uint64
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &m))
	require.NotZero(t, m.Fee, "manifest fee decoded")
	require.NotZero(t, m.TickSpacing, "manifest tick spacing decoded")
	var next *BridgePool
	if len(m.Next) > 0 {
		raw := m.Next
		if raw[0] == '"' {
			var nested string
			require.NoError(t, json.Unmarshal(raw, &nested))
			raw = []byte(nested)
		}
		require.NoError(t, json.Unmarshal(raw, &next))
	}
	client := ethrpc.New(endpoint).SetMulticallContract(common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))
	header, err := client.GetETHClient().HeaderByNumber(context.Background(), new(big.Int).SetUint64(m.Block))
	require.NoError(t, err)
	require.Equal(t, m.Timestamp, header.Time, "quote and execution timestamps")
	sim := bridgeSimulator(t, client, m.BridgePool, m.Block)
	var other *v4.PoolSimulator
	limits := sim.CalculateLimit()
	if next != nil {
		other = bridgeSimulator(t, client, *next, m.Block)
		for k, v := range other.CalculateLimit() {
			if previous, ok := limits[k]; ok {
				require.Equal(t, previous.String(), v.String(), "shared snapshot agrees")
			}
			limits[k] = v
		}
	}
	require.Equal(t, new(big.Int).SetUint64(m.AaveCash).String(), limits["ticker:143:aave:0x754704bc059f8c67012fed69bc8a327a5aafb603:cash"].String(), "quote mirror has the same Aave cash as Forge")
	inventory := swaplimit.NewInventory(sim.GetExchange(), limits)
	in, ok := new(big.Int).SetString(m.AmountIn, 10)
	require.True(t, ok)
	tokenOut := strings.ToLower(m.Token1)
	if strings.EqualFold(m.TokenIn, m.Token1) {
		tokenOut = strings.ToLower(m.Token0)
	}
	result, err := sim.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: strings.ToLower(m.TokenIn), Amount: in}, TokenOut: tokenOut, Limit: inventory})
	require.NoError(t, err)
	values := bridgeQuoteValues(result)
	for k := range limits {
		if strings.HasSuffix(k, ":cash") {
			values["before_"+k] = inventory.GetLimit(k).String()
		}
	}
	if m.RoundTrip {
		sim.UpdateBalance(pool.UpdateBalanceParams{TokenAmountIn: pool.TokenAmount{Token: strings.ToLower(m.TokenIn), Amount: in}, TokenAmountOut: *result.TokenAmountOut, SwapInfo: result.SwapInfo, SwapLimit: inventory})
		second, err := sim.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: *result.TokenAmountOut, TokenOut: strings.ToLower(m.TokenIn), Limit: inventory})
		require.NoError(t, err, "second quote uses simulated state, not a new RPC read")
		for k, v := range bridgeQuoteValues(second) {
			values["second_"+k] = v
		}
	}
	if other != nil {
		nextOut := strings.ToLower(next.Token1)
		if nextOut == tokenOut {
			nextOut = strings.ToLower(next.Token0)
		}
		params := pool.CalcAmountOutParams{TokenAmountIn: *result.TokenAmountOut, TokenOut: nextOut, Limit: inventory}
		fresh, err := other.CalcAmountOut(params)
		require.NoError(t, err, "second pool liquid before first leg")
		sim.UpdateBalance(pool.UpdateBalanceParams{TokenAmountIn: pool.TokenAmount{Token: strings.ToLower(m.TokenIn), Amount: in}, TokenAmountOut: *result.TokenAmountOut, SwapInfo: result.SwapInfo, SwapLimit: inventory})
		for k := range limits {
			if strings.HasSuffix(k, ":cash") {
				values["after_"+k] = inventory.GetLimit(k).String()
			}
		}
		second, err := other.CalcAmountOut(params)
		if err != nil {
			require.ErrorIs(t, err, ErrVaultIlliquid, "shared-cash rejection")
			values["secondRejected"] = "1"
			second = fresh // Preserve the stale quote to prove that trying it on-chain fails.
		}
		for k, v := range bridgeQuoteValues(second) {
			values["second_"+k] = v
		}
	}

	response, err := json.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("KYBER_BRIDGE_RESPONSE"), response, 0600))
}

func bridgeQuoteValues(result *pool.CalcAmountOutResult) map[string]string {
	q := result.SwapInfo.(v4.SwapInfo).HookSwapInfo.(Quote)
	return map[string]string{"output": result.TokenAmountOut.Amount.String(), "hookFee": q.HookFee.Dec(), "creatorTax": q.CreatorTax.Dec(), "reserve0": q.Swap.Balance0.Dec(), "reserve1": q.Swap.Balance1.Dec(), "sqrtPriceX96": q.Swap.SqrtPriceX96.Dec()}
}

func bridgeSimulator(t *testing.T, client *ethrpc.Client, m BridgePool, block uint64) *v4.PoolSimulator {
	t.Helper()
	addr := common.HexToAddress(m.Hook)
	static, err := json.Marshal(v4.StaticExtra{Fee: m.Fee, TickSpacing: int32(m.TickSpacing), HooksAddress: addr})
	require.NoError(t, err)
	p := entity.Pool{Address: strings.ToLower(m.PoolId), Type: v4.DexType, StaticExtra: string(static), BlockNumber: block, Tokens: []*entity.PoolToken{{Address: strings.ToLower(m.Token0), Swappable: true}, {Address: strings.ToLower(m.Token1), Swappable: true}}}
	param := &v4.HookParam{HookAddress: addr, BlockNumber: new(big.Int).SetUint64(block), RpcClient: client, Cfg: &v4.Config{StateViewAddress: "0x77395F3b2E73aE90843717371294fa97cC419D64"}, Pool: &p}
	h := NewHook(param).(*Hook)
	raw, err := h.Track(context.Background(), param)
	require.NoError(t, err, "track exact deployed accounting")
	fetch := v4.FetchRPCResult{Liquidity: big.NewInt(0), TickSpacing: int32(m.TickSpacing), Slot0: v4.Slot0Data{SqrtPriceX96: h.Pool.SqrtPriceX96.ToBig(), Tick: big.NewInt(int64(h.Pool.Tick)), LpFee: h.Pool.LpFee, ProtocolFee: h.Pool.ProtocolFee}, HookExtra: raw}
	extra, err := json.Marshal(fetch.ToExtra([]v4.Tick{}))
	require.NoError(t, err)
	p.Extra = string(extra)
	p.Reserves = entity.PoolReserves{h.Pool.Balance0.Dec(), h.Pool.Balance1.Dec()}
	v4.RegisterHooksFactory(NewHook, addr)
	sim, err := v4.NewPoolSimulator(p, valueobject.ChainIDMonad)
	require.NoError(t, err)
	return sim
}
