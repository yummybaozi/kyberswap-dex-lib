package tickermemehook

import (
	"context"
	"fmt"
	"math/big"
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
	_, err = quoteP(synth, sim, true, big.NewInt(1000))
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
	var fx []capturedPool
	require.NoError(t, json.Unmarshal([]byte(capturedFixtures), &fx))
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
			// Historical execution used a conservative input. The minimum-input
			// quote may now use less; retain the captured on-chain result unchanged.
			require.LessOrEqual(t, res.TokenAmountIn.Amount.Cmp(bi(c.AmountIn)), 0, "%s %+v", cp.Name, c)
			minimal, err := quoteP(cp.Pool, sim, c.ZeroForOne, res.TokenAmountIn.Amount)
			require.NoError(t, err)
			require.GreaterOrEqual(t, minimal.TokenAmountOut.Amount.Cmp(bi(c.Want)), 0)
			previous, err := quoteP(cp.Pool, sim, c.ZeroForOne, new(big.Int).Sub(res.TokenAmountIn.Amount, big.NewInt(1)))
			if err == nil {
				require.Less(t, previous.TokenAmountOut.Amount.Cmp(bi(c.Want)), 0)
			}
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

// Captured contract results replayed without RPC; values are unchanged from the original fixtures.
const capturedFixtures = `[
{
  "name":"E",
  "hook":"0x7fcc596401ff7c269dd57bfdda53617e95c66044",
  "pool":{
    "address":"0x8d1f2d254f3cdac09f2b5f14345c91887a566daa3176b595af9a0985d1fafeef",
    "swapFee":2000,
    "exchange":"uniswap-v4-ticker-memehook",
    "type":"uniswap-v4",
    "reserves":["64899946319192020392886466","13908416092447"],
    "tokens":[{"address":"0x27d60c6b6be3045e8008bef1f80031ac28324210","decimals":18,"swappable":true},{"address":"0x754704bc059f8c67012fed69bc8a327a5aafb603","decimals":6,"swappable":true}],
    "extra":"{\"hX\":{\"reg\":true,\"hF\":100,\"cT\":0},\"liquidity\":139534884618712817,\"sqrtPriceX96\":170340549445282407078,\"tick\":-399176,\"tickSpacing\":200,\"ticks\":[{\"index\":-887200,\"liquidityGross\":139534884618712817,\"liquidityNet\":139534884618712817},{\"index\":887200,\"liquidityGross\":139534884618712817,\"liquidityNet\":-139534884618712817}]}",
    "staticExtra":"{\"0x0\":[false,false],\"fee\":2000,\"tS\":200,\"hooks\":\"0x7fcc596401ff7c269dd57bfdda53617e95c66044\"}"
  },
  "cases":[
    {"z":true,"in":"1","out":"0"},
    {"z":true,"in":"100000000000000000","out":"0"},
    {"z":true,"in":"300000000000000000","out":"1"},
    {"z":true,"in":"10000000000000000000","out":"46"},
    {"z":true,"in":"3000000000000000000000000000000","out":"296993563"},
    {"in":"1","out":"0"},
    {"in":"2","out":"214169820711284985"},
    {"in":"10000000","out":"2068599411206149363663916"},
    {"in":"3000000000000000000000000000000","out":"64250946856000100188951153"}
  ],
  "exactOut":[
    {"z":true,"want":"9","in":"2167666935533246405","out":"10"},
    {"z":true,"want":"45664","in":"10000117724977081483573","out":"45665"},
    {"want":"9","in":"2","out":"214169820711284985"},
    {"want":"62375919211679837656381295","in":"10000000001","out":"62375919211862233341592748"}
  ],
  "seq":[
    {"z":true,"in":"1622498657979800509822161","out":"7229768","gas":212620,"after":{"hX":{"reg":true,"hF":100,"cT":0},"liquidity":139534884618712817,"sqrtPriceX96":166194008922661990418,"tick":-399669,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534884618712817,"liquidityNet":139534884618712817},{"index":887200,"liquidityGross":139534884618712817,"liquidityNet":-139534884618712817}]},"res":["66522444977171820902708627","13908408789652"]},
    {"z":true,"in":"1415371169727060019206566","out":"6025345","gas":161356,"after":{"hX":{"reg":true,"hF":100,"cT":0},"liquidity":139534884618712817,"sqrtPriceX96":162738249435483521549,"tick":-400090,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534884618712817,"liquidityNet":139534884618712817},{"index":887200,"liquidityGross":139534884618712817,"liquidityNet":-139534884618712817}]},"res":["67937816146898880921915193","13908402703445"]},
    {"in":"257563013026","out":"67177519388089029340829713","gas":225786,"after":{"hX":{"reg":true,"hF":100,"cT":0},"liquidity":139534884618712817,"sqrtPriceX96":146114998292790029790893,"tick":-264083,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534884618712817,"liquidityNet":139534884618712817},{"index":887200,"liquidityGross":139534884618712817,"liquidityNet":-139534884618712817}]},"res":["81735956909962395824574","14165965716471"]},
    {"z":true,"in":"1339933719835449111878","out":"4424565228","gas":160464,"after":{"hX":{"reg":true,"hF":100,"cT":0},"liquidity":139534884618712817,"sqrtPriceX96":143577345541981524145755,"tick":-264433,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534884618712817,"liquidityNet":139534884618712817},{"index":887200,"liquidityGross":139534884618712817,"liquidityNet":-139534884618712817}]},"res":["83075890629797844936452","14161496458665"]},
    {"in":"208257300862","out":"34388920213453344149850","gas":168875,"after":{"hX":{"reg":true,"hF":100,"cT":0},"liquidity":139534884618712817,"sqrtPriceX96":261589723849852456846432,"tick":-252434,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534884618712817,"liquidityNet":139534884618712817},{"index":887200,"liquidityGross":139534884618712817,"liquidityNet":-139534884618712817}]},"res":["48339607585905578118422","14369753759527"]},
    {"in":"191596716793","out":"12271608615689823570765","gas":163377,"after":{"hX":{"reg":true,"hF":100,"cT":0},"liquidity":139534884618712817,"sqrtPriceX96":370161111893844543763172,"tick":-245491,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534884618712817,"liquidityNet":139534884618712817},{"index":887200,"liquidityGross":139534884618712817,"liquidityNet":-139534884618712817}]},"res":["35944043327633029057044","14561350476320"]}
  ]
},
{
  "name":"F",
  "hook":"0x7fcc596401ff7c269dd57bfdda53617e95c66044",
  "pool":{
    "address":"0x2b21ece37d5134a5b43eb2892da1de767810ad15d7d8c8b3407e015209d3f39a",
    "swapFee":2000,
    "exchange":"uniswap-v4-ticker-memehook",
    "type":"uniswap-v4",
    "reserves":["13908416092447","64899946520432162380542185"],
    "tokens":[{"address":"0x754704bc059f8c67012fed69bc8a327a5aafb603","decimals":6,"swappable":true},{"address":"0xdc804cb7e2f700d22c711afd96fa294f7eec308b","decimals":18,"swappable":true}],
    "extra":"{\"hX\":{\"reg\":true,\"hF\":100,\"cT\":133},\"liquidity\":139534885067604109,\"sqrtPriceX96\":36850308133314700696920213526519746558,\"tick\":399175,\"tickSpacing\":200,\"ticks\":[{\"index\":-887200,\"liquidityGross\":139534885067604109,\"liquidityNet\":139534885067604109},{\"index\":887200,\"liquidityGross\":139534885067604109,\"liquidityNet\":-139534885067604109}]}",
    "staticExtra":"{\"0x0\":[false,false],\"fee\":2000,\"tS\":200,\"hooks\":\"0x7fcc596401ff7c269dd57bfdda53617e95c66044\"}"
  },
  "cases":[
    {"z":true,"in":"1","out":"0"},
    {"z":true,"in":"2","out":"211292589737785783"},
    {"z":true,"in":"10000000","out":"2040809136032335646896962"},
    {"z":true,"in":"13922651932","out":"62048109392245886898744304"},
    {"in":"1","out":"0"},
    {"in":"100000000000000000","out":"0"},
    {"in":"300000000000000000","out":"1"},
    {"in":"10000000000000000000","out":"46"},
    {"in":"65497796590732970427030","out":"294822"}
  ],
  "exactOut":[
    {"z":true,"want":"9","in":"2","out":"211292589737785783"},
    {"z":true,"want":"61537939875692048570223771","in":"10000000001","out":"61537939875871993890300265"},
    {"want":"9","in":"2167666935028867189","out":"10"},
    {"want":"45051","in":"10000117722646346408831","out":"45052"}
  ],
  "seq":[
    {"z":true,"in":"347710402311","out":"63333025352896923782320974","gas":248878,"after":{"hX":{"reg":true,"hF":100,"cT":133},"liquidity":139534885067604109,"sqrtPriceX96":31830163221255807257299458913420069,"tick":258084,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534885067604109,"liquidityNet":139534885067604109},{"index":887200,"liquidityGross":139534885067604109,"liquidityNet":-139534885067604109}]},"res":["14256126494758","56058578487938174213759"]},
    {"z":true,"in":"303321840314","out":"25497874112914917581805","gas":174827,"after":{"hX":{"reg":true,"hF":100,"cT":133},"liquidity":139534885067604109,"sqrtPriceX96":17007045091661633558704983540290049,"tick":245548,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534885067604109,"liquidityNet":139534885067604109},{"index":887200,"liquidityGross":139534885067604109,"liquidityNet":-139534885067604109}]},"res":["14559448335072","29952431141859626469514"]},
    {"in":"554674650775178267953","out":"11520686317","gas":235336,"after":{"hX":{"reg":true,"hF":100,"cT":133},"liquidity":139534885067604109,"sqrtPriceX96":17321360480577898564104177232328147,"tick":245914,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534885067604109,"liquidityNet":139534885067604109},{"index":887200,"liquidityGross":139534885067604109,"liquidityNet":-139534885067604109}]},"res":["14547652813094","30507105792634804737467"]},
    {"z":true,"in":"238486111690","out":"8093103253357559750237","gas":168073,"after":{"hX":{"reg":true,"hF":100,"cT":133},"liquidity":139534885067604109,"sqrtPriceX96":12616457435395845598866130780360241,"tick":239575,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534885067604109,"liquidityNet":139534885067604109},{"index":887200,"liquidityGross":139534885067604109,"liquidityNet":-139534885067604109}]},"res":["14786138924784","22220934754078892225707"]},
    {"in":"326778452265866062142","out":"12379455729","gas":167028,"after":{"hX":{"reg":true,"hF":100,"cT":133},"liquidity":139534885067604109,"sqrtPriceX96":12801631746444417458242954116227606,"tick":239867,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534885067604109,"liquidityNet":139534885067604109},{"index":887200,"liquidityGross":139534885067604109,"liquidityNet":-139534885067604109}]},"res":["14773464146727","22547713206344758287849"]},
    {"in":"300636176084596777171","out":"11076948316","gas":166900,"after":{"hX":{"reg":true,"hF":100,"cT":133},"liquidity":139534885067604109,"sqrtPriceX96":12971992112609103568869835993882896,"tick":240131,"tickSpacing":200,"ticks":[{"index":-887200,"liquidityGross":139534885067604109,"liquidityNet":139534885067604109},{"index":887200,"liquidityGross":139534885067604109,"liquidityNet":-139534885067604109}]},"res":["14762122948493","22848349382429355065020"]}
  ]
}
]`

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
