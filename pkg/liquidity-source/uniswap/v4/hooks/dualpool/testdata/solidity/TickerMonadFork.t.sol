// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import {Test} from "forge-std/Test.sol";
import {console2} from "forge-std/console2.sol";
import {TickerDualPoolHookV1} from "ticker-src/ticker/dualpool/TickerDualPoolHookV1.sol";
import {TickerDualPoolHookBase} from "ticker-src/ticker/dualpool/TickerDualPoolHookBase.sol";
import {TickerDualPoolSweeper} from "ticker-src/ticker/dualpool/TickerDualPoolSweeper.sol";
import {TickerVault} from "ticker-src/ticker/vault/TickerVault.sol";
import {ITickerYieldRecipientSource} from "ticker-src/ticker/interfaces/ITickerYield.sol";
import {PonsV2FeeEscrow} from "ticker-src/v2/PonsV2FeeEscrow.sol";
import {FeePolicySnapshot} from "ticker-src/v2/interfaces/ILaunchpadV2.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";
import {TimelockController} from "@openzeppelin/contracts/governance/TimelockController.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {IERC4626} from "@openzeppelin/contracts/interfaces/IERC4626.sol";
import {MockERC20} from "solmate/src/test/utils/mocks/MockERC20.sol";
import {IPoolManager} from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import {IHooks} from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import {PoolKey} from "@uniswap/v4-core/src/types/PoolKey.sol";
import {PoolIdLibrary} from "@uniswap/v4-core/src/types/PoolId.sol";
import {Currency} from "@uniswap/v4-core/src/types/Currency.sol";
import {BalanceDelta} from "@uniswap/v4-core/src/types/BalanceDelta.sol";
import {SwapParams} from "@uniswap/v4-core/src/types/PoolOperation.sol";
import {PoolSwapTest} from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import {TickMath} from "@uniswap/v4-core/src/libraries/TickMath.sol";
import {StateLibrary} from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import {DualPoolHook} from "@uniswap/v4-hooks-public/src/alf/DualPoolHook.sol";
import {LiquidityBucket} from "@uniswap/v4-hooks-public/src/alf/types/Distribution.sol";

contract ForkYieldRecipient is ITickerYieldRecipientSource {
    function yieldRecipientOf(address) external pure returns (address) { return address(0xBEEF); }
}

// No broadcast: all deployments and balance changes exist only in Foundry's fork.
contract TickerMonadFork is Test {
    using PoolIdLibrary for PoolKey;
    using StateLibrary for IPoolManager;
    IPoolManager constant manager = IPoolManager(0x188d586Ddcf52439676Ca21A244753fA19F9Ea8e);
    IERC20 constant usdc = IERC20(0x754704Bc059F8C67012fEd69BC8A327a5aafb603);
    IERC4626 constant strategy = IERC4626(0xC554aFfE2f581F5E0811e0D42D484ECaC5c6B8e2);
    TickerDualPoolHookBase hook;
    TickerVault vault;
    MockERC20 token;
    PoolSwapTest router;
    PoolKey key;

    function setUp() public {
        vm.createSelectFork(vm.envOr("MONAD_RPC_URL",string("https://rpc.monad.xyz")),111021450);
        assertEq(block.chainid,143);
        assertEq(strategy.asset(),address(usdc));
        PonsV2FeeEscrow escrow = new PonsV2FeeEscrow();
        TickerDualPoolSweeper sweeper = new TickerDualPoolSweeper(manager);
        address[] memory seats = new address[](1); seats[0]=address(this);
        TimelockController timelock = new TimelockController(1 days,seats,seats,address(0));
        TickerDualPoolHookV1 impl = new TickerDualPoolHookV1(manager,100_000,type(uint64).max,escrow,sweeper,address(timelock));
        bytes memory init = abi.encodeCall(TickerDualPoolHookBase.initialize,(address(this),address(this)));
        bytes32 initHash = keccak256(abi.encodePacked(type(ERC1967Proxy).creationCode,abi.encode(address(impl),init)));
        bytes32 salt;
        address predicted;
        for (uint256 i; ; ++i) {
            salt=bytes32(i);
            predicted=address(uint160(uint256(keccak256(abi.encodePacked(bytes1(0xff),address(this),salt,initHash)))));
            if (uint160(predicted) & 0x3fff == 0x2ac4) break;
        }
        hook=TickerDualPoolHookBase(address(new ERC1967Proxy{salt:salt}(address(impl),init)));
        assertEq(address(hook),predicted);
        token=new MockERC20("Local Kyber test","KTEST",18);
        vault=new TickerVault(usdc,strategy,new ForkYieldRecipient(),10_000,"Local USDC","kUSDC");
        router=new PoolSwapTest(manager);
        bool token0=address(token)<address(usdc);
        key=PoolKey(Currency.wrap(token0?address(token):address(usdc)),Currency.wrap(token0?address(usdc):address(token)),2000,10,IHooks(address(hook)));
        int24 start=token0?int24(-322380):int24(322380);
        LiquidityBucket[] memory buckets=new LiquidityBucket[](1);
        buckets[0]=LiquidityBucket(start-10000,start+10000,10000);
        hook.initializePool(key,DualPoolHook.PoolConfig(TickMath.getSqrtPriceAtTick(start),buckets,false,
            token0?IERC4626(address(0)):IERC4626(address(vault)),token0?IERC4626(address(vault)):IERC4626(address(0)),0));
        hook.registerPool(key,address(token),address(this),address(this),50,false,
            FeePolicySnapshot(address(this),3000,5000,100,300));
        token.mint(address(this),2_000_000e18);
        deal(address(usdc),address(this),20_000e6);
        token.approve(address(hook),type(uint256).max);
        usdc.approve(address(hook),type(uint256).max);
        hook.bootstrap(key,token0?1_000_000e18:10_000e6,token0?10_000e6:1_000_000e18);
        token.approve(address(router),type(uint256).max);
        usdc.approve(address(router),type(uint256).max);
        vm.roll(block.number+1);
    }

    function test_freshAavePoolTradesBothWays() public {
        bool buyZero=address(usdc)<address(token);
        for(uint256 i;i<2;++i) {
            (uint256 b0,uint256 b1)=hook.getEffectiveLiquidity(key);
            (uint160 initialPrice,int24 initialTick,,)=manager.getSlot0(key.toId());
            bool direction=i==0?buyZero:!buyZero;
            uint256 input=i==0?100e6:1000e18;
            BalanceDelta delta=router.swap(key,SwapParams(direction,-int256(input),direction?TickMath.MIN_SQRT_PRICE+1:TickMath.MAX_SQRT_PRICE-1),PoolSwapTest.TestSettings(false,false),"");
            int128 output=direction?delta.amount1():delta.amount0();
            assertGt(output,0);
            (uint160 price,int24 tick,,)=manager.getSlot0(key.toId());
            console2.log("FORK quote: direction,input,b0,b1,initialPrice,initialTick,net,price,tick");
            console2.log(direction);console2.log(input);console2.log(b0);console2.log(b1);
            console2.log(uint256(initialPrice));console2.logInt(initialTick);
            console2.logInt(output);console2.log(uint256(price));console2.logInt(tick);
        }
        assertGt(strategy.balanceOf(address(vault)),0,"quote was not redeposited in Aave strategy");
        assertEq(vault.yieldFeeBps(),10000);
    }
}
