package tickerdualpool

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
)

func TestBaseTrackerDecodesPinnedSnapshot(t *testing.T) {
	readABI := func(path string) abi.ABI {
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		a, err := abi.JSON(strings.NewReader(string(b)))
		require.NoError(t, err)
		return a
	}
	base := readABI("../dualpool/abis/DualPoolHook.json")
	view := readABI("../dualpool/abis/StateView.json")
	pack := func(a abi.ABI, name string, values ...any) []byte {
		out, err := a.Methods[name].Outputs.Pack(values...)
		require.NoError(t, err)
		return out
	}
	s := fixtureState()
	distribution := []struct {
		TickLower, TickUpper *big.Int
		WeightBps            uint16
	}{{big.NewInt(-10000), big.NewInt(10000), 10000}}
	vault := common.HexToAddress("0x1234")
	empty := common.Address{}
	batches := [][][]byte{
		{pack(base, "getEffectiveLiquidity", s.Pool.Balance0.ToBig(), s.Pool.Balance1.ToBig()),
			pack(base, "getDistribution", distribution), pack(base, "livePools", true),
			pack(view, "getSlot0", s.Pool.SqrtPriceX96.ToBig(), big.NewInt(0), big.NewInt(0), big.NewInt(2000))},
		{pack(tickerABI, "totalAssets", s.Total0.ToBig(), s.Total1.ToBig()),
			pack(tickerABI, "launches", true, true, empty, empty, empty, empty, empty, uint16(50), uint16(3000), uint16(5000), uint16(100), uint16(300), false),
			pack(tickerABI, "vaults", empty), pack(tickerABI, "vaults", vault)},
	}
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	bytesType, err := abi.NewType("bytes[]", "", nil)
	require.NoError(t, err)
	aggregate := abi.Arguments{{Type: uintType}, {Type: bytesType}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "decode", 400)
			return
		}
		if request.Method != "eth_call" || len(request.Params) < 2 || string(request.Params[1]) != `"0x7b"` {
			t.Errorf("not pinned: %s", request.Params)
			http.Error(w, "block", 400)
			return
		}
		if calls >= len(batches) {
			t.Error("unexpected request")
			http.Error(w, "extra", 400)
			return
		}
		out, err := aggregate.Pack(big.NewInt(123), batches[calls])
		if err != nil {
			t.Error(err)
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": hexutil.Encode(out)})
	}))
	defer server.Close()
	address := common.HexToAddress("0x12ac4")
	static, err := json.Marshal(uniswapv4.StaticExtra{Fee: 2000, TickSpacing: 10, HooksAddress: address})
	require.NoError(t, err)
	p := &uniswapv4.HookParam{HookAddress: address, BlockNumber: big.NewInt(123),
		Cfg:       &uniswapv4.Config{StateViewAddress: "0x0000000000000000000000000000000000000003"},
		RpcClient: ethrpc.New(server.URL).SetMulticallContract(common.HexToAddress("0x4567")),
		Pool:      &entity.Pool{Address: common.HexToHash("0x1").Hex(), StaticExtra: string(static), Tokens: []*entity.PoolToken{{Address: "0x1"}, {Address: "0x2"}}}}
	h := NewHook(p).(*Hook)
	raw, err := h.trackBase(context.Background(), p)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	var next Snapshot
	require.NoError(t, json.Unmarshal(raw, &next))
	require.Equal(t, uint64(123), next.BlockNumber)
	require.Equal(t, uint16(100), next.HookFeeBps)
	require.Equal(t, uint16(50), next.CreatorTaxBps)
	require.Equal(t, vault, next.Vaults[1])
	require.Equal(t, s.Total1.Dec(), next.Total1.Dec())
	require.Equal(t, s.Pool.Balance0.Dec(), next.State.Pool.Balance0.Dec())
}
