package tickermemehook

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	uniswapv4 "github.com/KyberNetwork/kyberswap-dex-lib/pkg/liquidity-source/uniswap/v4"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

// The hand-set pool: the shape a graduation leaves behind, one full-range
// position (TickerLaunchFactory._fullRangeTicks) at price 1 with 1e18 liquidity,
// tick spacing 60 and a 30 bps LP fee.
const (
	token0 = "0x1000000000000000000000000000000000000001"
	token1 = "0x2000000000000000000000000000000000000002"
	poolID = "0x0f8e0a2a1a4c6a8f2b3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293"
)

var synth = entity.Pool{Tokens: []*entity.PoolToken{{Address: token0, Decimals: 18, Swappable: true},
	{Address: token1, Decimals: 6, Swappable: true}}}

var testHook = common.HexToAddress("0x7e570000000000000000000000000000000000c0")

func init() { RegisterHookAddresses(testHook) }

// RegisterHookAddresses registers the factory for a hook that is not in
// HookAddresses: the hand-set test hook and the fork deployment the fixtures
// were captured from. It is exported from a test file for serialization_test.go.
func RegisterHookAddresses(addresses ...common.Address) bool {
	return uniswapv4.RegisterHooksFactory(NewHook, addresses...)
}

func hookExtra(e Extra) string {
	return fmt.Sprintf(`{"reg":%t,"hF":%d,"cT":%d}`, e.Registered, e.HookFeeBps, e.CreatorTaxBps)
}

// newSim builds the pool with the hook's terms, or without the hook when hooked is false.
func newSim(t testing.TB, e Extra, hooked bool) *uniswapv4.PoolSimulator {
	extra := `{"liquidity":1000000000000000000,"tickSpacing":60,"tick":0,"sqrtPriceX96":79228162514264337593543950336,` +
		`"ticks":[{"index":-887220,"liquidityGross":1000000000000000000,"liquidityNet":1000000000000000000},` +
		`{"index":887220,"liquidityGross":1000000000000000000,"liquidityNet":-1000000000000000000}]`
	hooks, exchange := common.Address{}, "uniswap-v4"
	if hooked {
		extra += `,"hX":` + hookExtra(e)
		hooks, exchange = testHook, string(Exchange)
	}
	p := entity.Pool{
		Address: poolID, Exchange: exchange, Type: "uniswap-v4", Tokens: synth.Tokens, Extra: extra + "}", SwapFee: 3000,
		Reserves:    entity.PoolReserves{"1000000000000000000000000", "1000000000000000000000000"},
		StaticExtra: fmt.Sprintf(`{"0x0":[false,false],"fee":3000,"tS":60,"hooks":"%s"}`, hooks.Hex()),
	}
	sim, err := uniswapv4.NewPoolSimulator(p, valueobject.ChainIDMonad)
	require.NoError(t, err)
	return sim
}

func bi(s string) *big.Int { v, _ := new(big.Int).SetString(s, 10); return v }

func TestSkim_RoundsEachPartSeparately(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		reg      bool
		fee, tax uint16
		gross    int64
		want     int64
	}{
		{true, 100, 100, 150, 2},        // 1.5 + 1.5 floors to 1 + 1; a combined 200 bps would take 3
		{true, 100, 133, 199, 3},        // 1.99 + 2.6467 floors to 1 + 2; combined 233 bps would take 4
		{true, 100, 0, 99, 0},           // dust: 0.99 floors to 0
		{true, 100, 0, 100, 1},          // first size that pays a fee
		{true, 0, 0, 1_000_000, 0},      // no terms
		{false, 100, 133, 1_000_000, 0}, // unregistered pool is not skimmed
		{true, 1000, 1000, 10, 2},
		{true, 1000, 1000, 9, 0},
	} {
		e := Extra{Registered: c.reg, HookFeeBps: c.fee, CreatorTaxBps: c.tax}
		require.Equal(t, c.want, e.Skim(big.NewInt(c.gross)).Int64(), "%+v", c)
	}
}

// An exact-in quote is the plain pool's output less the skim on that output.
func TestQuote_ExactIn_SkimsGrossOutput(t *testing.T) {
	t.Parallel()
	plain := newSim(t, Extra{}, false)
	for _, e := range []Extra{{true, 100, 0}, {true, 100, 133}, {true, 0, 0}, {false, 100, 133}} {
		hooked := newSim(t, e, true)
		for _, zeroForOne := range []bool{true, false} {
			for _, in := range []string{"1", "150", "199", "123456789", "1000000000000000000", "77000000000000000000000"} {
				want, err := quoteP(synth, plain, zeroForOne, bi(in))
				got, gotErr := quoteP(synth, hooked, zeroForOne, bi(in))
				if err != nil { // dust the plain pool refuses (zero output) is refused with the hook too
					require.EqualError(t, gotErr, err.Error(), "%+v z=%t in=%s", e, zeroForOne, in)
					continue
				}
				require.NoError(t, gotErr, "%+v z=%t in=%s", e, zeroForOne, in)
				gross := want.TokenAmountOut.Amount
				require.Equal(t, new(big.Int).Sub(gross, e.Skim(gross)).String(), got.TokenAmountOut.Amount.String(),
					"%+v z=%t in=%s", e, zeroForOne, in)
			}
		}
	}
}

func TestCalcAmountIn_RequoteDeliversRequestedOutput(t *testing.T) {
	t.Parallel()
	for _, e := range []Extra{{true, 100, 0}, {true, 100, 133}, {true, 1000, 1000}, {true, 0, 0}} {
		sim := newSim(t, e, true)
		for _, zeroForOne := range []bool{true, false} {
			for _, out := range []string{"9", "150", "199", "123456789", "400000000000000000"} {
				in, outTok := token0, token1
				if !zeroForOne {
					in, outTok = token1, token0
				}
				res, err := sim.CalcAmountIn(pool.CalcAmountInParams{
					TokenAmountOut: pool.TokenAmount{Token: outTok, Amount: bi(out)}, TokenIn: in})
				require.NoError(t, err, "%+v z=%t out=%s", e, zeroForOne, out)
				back, err := quoteP(synth, sim, zeroForOne, res.TokenAmountIn.Amount)
				require.NoError(t, err)
				require.GreaterOrEqual(t, back.TokenAmountOut.Amount.Cmp(bi(out)), 0,
					"%+v z=%t: in %s gives %s, want >= %s", e, zeroForOne, res.TokenAmountIn.Amount, back.TokenAmountOut.Amount, out)
			}
		}
	}
}

func TestCalcAmountIn_RejectsTermsAtOrAbove100Percent(t *testing.T) {
	t.Parallel()
	sim := newSim(t, Extra{true, 5000, 5000}, true)
	_, err := sim.CalcAmountIn(pool.CalcAmountInParams{
		TokenAmountOut: pool.TokenAmount{Token: token1, Amount: big.NewInt(1000)}, TokenIn: token0})
	require.ErrorIs(t, err, ErrInvalidTerms)
}

// Terms are frozen at registerPool time, so a registered pool is not re-read.
func TestTrack_RegisteredIsNotRefetched(t *testing.T) {
	t.Parallel()
	h, ok := uniswapv4.GetHook(testHook, &uniswapv4.HookParam{HookExtra: uniswapv4.HookExtra(hookExtra(Extra{true, 100, 133}))})
	require.True(t, ok)
	raw, err := h.Track(context.Background(), &uniswapv4.HookParam{}) // no RPC client: a fetch would panic
	require.NoError(t, err)
	require.JSONEq(t, `{"reg":true,"hF":100,"cT":133}`, string(raw))
}

// TestFixtures_Replay replays what the live differential test captured with
// TICKER_MEMEHOOK_CAPTURE=1: tracked state of pools E and F, the amounts the chain
// returned for independent swaps, CalcAmountIn results executed exact-in, and
// the mined sequential swaps, each quoted from the state tracked before it.
func TestFixtures_Replay(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	var fx []capturedPool
	require.NoError(t, json.Unmarshal(raw, &fx))
	require.Len(t, fx, 2)
	for _, cp := range fx {
		RegisterHookAddresses(common.HexToAddress(cp.Hook))
		sim, err := uniswapv4.NewPoolSimulator(cp.Pool, valueobject.ChainIDMonad)
		require.NoError(t, err)
		for _, c := range cp.Cases {
			res, err := quoteP(cp.Pool, sim, c.ZeroForOne, bi(c.AmountIn))
			if c.AmountOut == "" || c.AmountOut == "0" { // reverted, partially filled, or zero output: the simulator refuses
				require.Error(t, err, "%s %+v", cp.Name, c)
				continue
			}
			require.NoError(t, err, "%s %+v", cp.Name, c)
			require.Equal(t, c.AmountOut, res.TokenAmountOut.Amount.String(), "%s %+v", cp.Name, c)
		}
		for _, c := range cp.ExactOut {
			in, out := cp.Pool.Tokens[0].Address, cp.Pool.Tokens[1].Address
			if !c.ZeroForOne {
				in, out = out, in
			}
			res, err := sim.CalcAmountIn(pool.CalcAmountInParams{
				TokenAmountOut: pool.TokenAmount{Token: out, Amount: bi(c.Want)}, TokenIn: in})
			require.NoError(t, err, "%s %+v", cp.Name, c)
			require.Equal(t, c.AmountIn, res.TokenAmountIn.Amount.String(), "%s %+v", cp.Name, c)
			requoted, err := quoteP(cp.Pool, sim, c.ZeroForOne, bi(c.AmountIn))
			require.NoError(t, err)
			require.Equal(t, c.Executed, requoted.TokenAmountOut.Amount.String(), "%s %+v", cp.Name, c)
			require.GreaterOrEqual(t, bi(c.Executed).Cmp(bi(c.Want)), 0, "%s %+v", cp.Name, c)
		}
		// Each mined swap quoted from the state tracked before it must be exact, and
		// so must a simulator carried through the whole sequence with UpdateBalance
		// alone: the pool is an ordinary V4 pool, so the curve math is the whole state.
		p, carried := cp.Pool, sim.CloneState()
		for i, st := range cp.Seq {
			stepSim, err := uniswapv4.NewPoolSimulator(p, valueobject.ChainIDMonad)
			require.NoError(t, err)
			res, err := quoteP(cp.Pool, stepSim, st.ZeroForOne, bi(st.AmountIn))
			require.NoError(t, err, "%s step %d", cp.Name, i)
			require.Equal(t, st.AmountOut, res.TokenAmountOut.Amount.String(), "%s step %d", cp.Name, i)

			resC, err := quoteP(cp.Pool, carried, st.ZeroForOne, bi(st.AmountIn))
			require.NoError(t, err, "%s step %d", cp.Name, i)
			require.Equal(t, st.AmountOut, resC.TokenAmountOut.Amount.String(), "%s step %d carried", cp.Name, i)
			tokenIn := cp.Pool.Tokens[1].Address
			if st.ZeroForOne {
				tokenIn = cp.Pool.Tokens[0].Address
			}
			carried.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: resC.SwapInfo,
				TokenAmountIn: pool.TokenAmount{Token: tokenIn, Amount: bi(st.AmountIn)}, TokenAmountOut: *resC.TokenAmountOut})
			p.Extra, p.Reserves = string(st.After), st.Reserves
		}
	}
}

func quoteP(p entity.Pool, sim pool.IPoolSimulator, zeroForOne bool, in *big.Int) (*pool.CalcAmountOutResult, error) {
	a, b := p.Tokens[0].Address, p.Tokens[1].Address
	if !zeroForOne {
		a, b = b, a
	}
	return sim.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: a, Amount: in}, TokenOut: b})
}

// Captured fixtures, written by the live differential test and replayed hermetically.
const fixturePath = "testdata/fixtures.json"

// capturedCase is one exact-in swap executed as eth_call. Out is what the chain
// returned; empty means the chain reverted or consumed less than AmountIn, and
// "0" pairs with a zero-output refusal by the simulator.
type capturedCase struct {
	ZeroForOne bool   `json:"z,omitempty"`
	AmountIn   string `json:"in"`
	AmountOut  string `json:"out,omitempty"`
}

// capturedExactOut is a CalcAmountIn result executed as exact-in.
type capturedExactOut struct {
	ZeroForOne bool   `json:"z,omitempty"`
	Want       string `json:"want"`
	AmountIn   string `json:"in"`
	Executed   string `json:"out"`
}

// capturedStep is one mined swap of the sequential run and the state tracked after
// it: the pool extra and the PoolManager's balances of both currencies.
type capturedStep struct {
	ZeroForOne bool                `json:"z,omitempty"`
	AmountIn   string              `json:"in"`
	AmountOut  string              `json:"out"`
	GasUsed    uint64              `json:"gas"`
	After      json.RawMessage     `json:"after"`
	Reserves   entity.PoolReserves `json:"res"`
}

type capturedPool struct {
	Name     string             `json:"name"`
	Hook     string             `json:"hook"`
	Pool     entity.Pool        `json:"pool"`
	Cases    []capturedCase     `json:"cases"`
	ExactOut []capturedExactOut `json:"exactOut,omitempty"`
	Seq      []capturedStep     `json:"seq,omitempty"`
}

func TestCloneStateAndRepeatedQuotesAreIndependent(t *testing.T) {
	original := newSim(t, Extra{true, 100, 133}, true)
	params := pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: token0, Amount: big.NewInt(1_000_000_000_000)}, TokenOut: token1}
	first, err := original.CalcAmountOut(params)
	require.NoError(t, err)
	repeated, err := original.CalcAmountOut(params)
	require.NoError(t, err)
	require.Equal(t, first.TokenAmountOut.Amount.String(), repeated.TokenAmountOut.Amount.String())
	cloned := original.CloneState()
	cloned.UpdateBalance(pool.UpdateBalanceParams{TokenAmountIn: params.TokenAmountIn, TokenAmountOut: *first.TokenAmountOut, SwapInfo: first.SwapInfo})
	changed, err := cloned.CalcAmountOut(params)
	require.NoError(t, err)
	require.Less(t, changed.TokenAmountOut.Amount.Cmp(first.TokenAmountOut.Amount), 0)
	untouched, err := original.CalcAmountOut(params)
	require.NoError(t, err)
	require.Equal(t, first.TokenAmountOut.Amount.String(), untouched.TokenAmountOut.Amount.String())
}

func TestMalformedTermsAreRejected(t *testing.T) {
	h := NewHook(&uniswapv4.HookParam{HookExtra: uniswapv4.HookExtra(`{"hF":"bad"}`)})
	_, err := h.BeforeSwap(&uniswapv4.BeforeSwapParams{CalcOut: true, AmountSpecified: big.NewInt(100)})
	require.Error(t, err)
	_, err = h.Track(context.Background(), &uniswapv4.HookParam{})
	require.Error(t, err)
}

func TestProductionAddressesAreUnregistered(t *testing.T) {
	for _, address := range []string{"0x223F8B1b43AEEc86d2659cCe36169f72f7FC2044", "0x047ea2f55148b7601FDDe0d28588B55c09192ac4"} {
		_, found := uniswapv4.HookFactories[common.HexToAddress(address)]
		require.False(t, found)
	}
}
