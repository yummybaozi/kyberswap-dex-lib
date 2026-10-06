package dualpool

import (
	"testing"

	v3Utils "github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"
	"github.com/holiman/uint256"
)

// Independent reproduction of v4's uninitialized tick-word boundary, spacing=200.
func TestReauditWordBoundary(t *testing.T) {
	var p, lower, upper, boundary uint256.Int
	if err := v3Utils.GetSqrtRatioAtTickV2(-256400, &p); err != nil {
		t.Fatal(err)
	}
	if err := v3Utils.GetSqrtRatioAtTickV2(-887200, &lower); err != nil {
		t.Fatal(err)
	}
	if err := v3Utils.GetSqrtRatioAtTickV2(887200, &upper); err != nil {
		t.Fatal(err)
	}
	if err := v3Utils.GetSqrtRatioAtTickV2(-256200, &boundary); err != nil {
		t.Fatal(err)
	}
	b0 := uint256.MustFromDecimal("1000000000000000000000")
	b1 := uint256.NewInt(10000000000)
	liq, err := liquidityForAmounts(&p, &lower, &upper, b0, b1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("liq=%s sqrt=%s", liq.Dec(), p.Dec())
	for _, amount := range []uint64{100000000} {
		in := uint256.NewInt(amount)
		direct, err := swapExactIn([]position{{tickLower: -887200, tickUpper: 887200, sqrtLower: &lower, sqrtUpper: &upper, liquidity: liq}}, &p, -256400, false, in, 2000)
		if err != nil {
			t.Fatal(err)
		}
		next, a1, o1, f1, err := computeSwapStepExactIn(&p, &boundary, liq, in, 2000)
		if err != nil {
			t.Fatal(err)
		}
		remaining := new(uint256.Int).Sub(in, new(uint256.Int).Add(a1, f1))
		out := o1.Clone()
		end := next
		if !remaining.IsZero() {
			var o2 *uint256.Int
			end, _, o2, _, err = computeSwapStepExactIn(next, &upper, liq, remaining, 2000)
			if err != nil {
				t.Fatal(err)
			}
			out.Add(out, o2)
		}
		t.Logf("inputUSDCraw=%d directOut=%s bitmapOut=%s directSqrt=%s bitmapSqrt=%s crossed=%v", amount, direct.amountOut.Dec(), out.Dec(), direct.sqrtPriceX96.Dec(), end.Dec(), !remaining.IsZero())
		if remaining.IsZero() {
			t.Fatal("fixture must cross bitmap boundary")
		}
		if direct.amountOut.Dec() != "13428054353390492650" {
			t.Fatal("unexpected Kyber output", direct.amountOut.Dec())
		}
		if out.Dec() != "13428054105542917337" {
			t.Fatal("unexpected bitmap output", out.Dec())
		}
		if direct.amountOut.Cmp(out) <= 0 {
			t.Fatal("expected overquote")
		}
	}
}
