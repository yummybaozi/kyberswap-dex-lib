// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import {TickerDualPoolLpFeeTest} from "ticker-tests/dualpool/TickerDualPoolLpFee.t.sol";
import {BalanceDelta} from "@uniswap/v4-core/src/types/BalanceDelta.sol";
import {StateLibrary} from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import {IPoolManager} from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import {PoolIdLibrary} from "@uniswap/v4-core/src/types/PoolId.sol";
import {console2} from "forge-std/console2.sol";

// Uses the pinned Ticker repository's real proxy, hook, PoolManager, and vault fixture.
// Output includes backing after teardown: rounding dust must not be hidden by approximate assertions.
contract TickerQuoteFixture is TickerDualPoolLpFeeTest {
    using StateLibrary for IPoolManager;
    using PoolIdLibrary for *;

    function test_exportKyberQuoteFixtures() public {
        uint256[4] memory amounts = [uint256(1001), 1e18, 100_000e18, 400_000e18];
        for (uint256 side; side < 2; ++side) {
            for (uint256 i; i < amounts.length; ++i) {
                uint256 snapshot = vm.snapshotState();
                bool zeroForOne = side == 0;
                (uint256 b0, uint256 b1) = hook.getEffectiveLiquidity(pk);
                BalanceDelta delta = _trade(zeroForOne, amounts[i]);
                (uint160 price, int24 tick,,) = manager.getSlot0(id);
                (uint256 after0, uint256 after1) = hook.totalAssets(pk);
                address out = zeroForOne ? address(quote) : address(memecoin);
                console2.log("FIXTURE side,input,before0,before1,net,fee,tax,price,tick,after0,after1");
                console2.log(side);
                console2.log(amounts[i]);
                console2.log(b0);
                console2.log(b1);
                console2.log(zeroForOne ? _abs(delta.amount1()) : _abs(delta.amount0()));
                console2.log(hook.pendingFees(id, out));
                console2.log(hook.pendingCreatorTax(id, out));
                console2.log(uint256(price));
                console2.logInt(int256(tick));
                console2.log(after0);
                console2.log(after1);
                assertTrue(vm.revertToState(snapshot));
            }
        }
    }
    function test_exportSequentialQuotes() public {
        for (uint256 i; i < 4; ++i) {
            bool direction = i % 2 == 0;
            uint256 amount = (i + 1) * 50_000e18;
            BalanceDelta delta = _trade(direction, amount);
            (uint160 price,int24 tick,,) = manager.getSlot0(id);
            (uint256 a0,uint256 a1) = hook.totalAssets(pk);
            console2.log("SEQUENTIAL direction,input,net,price,tick,after0,after1");
            console2.log(direction);console2.log(amount);
            console2.log(direction ? _abs(delta.amount1()) : _abs(delta.amount0()));
            console2.log(uint256(price));console2.logInt(tick);console2.log(a0);console2.log(a1);
        }
    }
}
