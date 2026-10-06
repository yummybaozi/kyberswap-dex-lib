// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import {Vm} from "forge-std/Vm.sol";
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
import {PoolId, PoolIdLibrary} from "@uniswap/v4-core/src/types/PoolId.sol";
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
    address[] exportAccounts;
    mapping(address=>bytes32[]) exportSlots;

    function setUp() public {
        vm.createSelectFork(vm.envOr("MONAD_RPC_URL",string("https://rpc.monad.xyz")),111021450);
        vm.startStateDiffRecording();
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
        uint256 bucketCount=vm.envOr("KYBER_FIXTURE_BUCKETS",uint256(1));
        require(bucketCount==1||bucketCount==8,"fixture supports one or eight buckets");
        LiquidityBucket[] memory buckets=new LiquidityBucket[](bucketCount);
        for(uint256 i;i<bucketCount;++i) {
            int24 width=int24(int256(10000+10*i));
            buckets[i]=LiquidityBucket(start-width,start+width,uint16(10000/bucketCount));
        }
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
        exportAccounts.push(address(router));
        exportAccounts.push(address(hook));exportAccounts.push(address(impl));
        exportAccounts.push(address(vault));exportAccounts.push(address(token));
        exportAccounts.push(address(manager));exportAccounts.push(address(usdc));
        exportAccounts.push(address(vault.recipientSource()));
        exportAccounts.push(address(escrow));exportAccounts.push(address(sweeper));
        exportAccounts.push(address(timelock));exportAccounts.push(address(router));
        Vm.AccountAccess[] memory accesses=vm.stopAndReturnStateDiff();
        for(uint256 i;i<accesses.length;++i){
            for(uint256 j;j<accesses[i].storageAccesses.length;++j){
                Vm.StorageAccess memory item=accesses[i].storageAccesses[j];
                if(item.isWrite && !item.reverted) {
                    exportSlots[item.account].push(item.slot);
                    bool seen;
                    for(uint256 k;k<exportAccounts.length;++k) if(exportAccounts[k]==item.account) seen=true;
                    if(!seen) exportAccounts.push(item.account);
                }
            }
        }
        vm.roll(block.number+1);
    }

    function test_freshAavePoolTradesBothWays() public {
        bool buyZero=address(usdc)<address(token);
        for(uint256 i;i<2;++i) {
            (uint256 b0,uint256 b1)=hook.getEffectiveLiquidity(key);
            (uint160 initialPrice,int24 initialTick,,)=manager.getSlot0(key.toId());
            bool direction=i==0?buyZero:!buyZero;
            uint256 input=i==0?100e6:1000e18;
            _dumpManager("BEFORE_MANAGER");
            _dumpVault("BEFORE_VAULT");
            BalanceDelta delta=router.swap(key,SwapParams(direction,-int256(input),direction?TickMath.MIN_SQRT_PRICE+1:TickMath.MAX_SQRT_PRICE-1),PoolSwapTest.TestSettings(false,false),"");
            _dumpManager("AFTER_MANAGER");
            _dumpVault("AFTER_VAULT");
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
    function test_depositDisabledKeepsRawCashAndStillTrades() public {
        // Simulate the strategy refusing deposits while withdrawals remain live.
        // This checks Ticker's catch behavior; it does not claim to change Aave governance.
        vm.mockCall(address(strategy),abi.encodeWithSelector(IERC4626.maxDeposit.selector,address(vault)),abi.encode(uint256(0)));
        bool buyZero=address(usdc)<address(token);
        BalanceDelta first=router.swap(key,SwapParams(buyZero,-int256(100e6),buyZero?TickMath.MIN_SQRT_PRICE+1:TickMath.MAX_SQRT_PRICE-1),PoolSwapTest.TestSettings(false,false),"");
        assertGt(buyZero?first.amount1():first.amount0(),0);
        bytes32 partition=keccak256(abi.encode(key.toId(),address(usdc)));
        uint256 packed=uint256(vm.load(address(hook),keccak256(abi.encode(partition,uint256(5)))));
        assertGt(uint256(uint128(packed)),0,"failed deposit must leave raw USDC in pool inventory");
        BalanceDelta second=router.swap(key,SwapParams(!buyZero,-int256(1000e18),!buyZero?TickMath.MIN_SQRT_PRICE+1:TickMath.MAX_SQRT_PRICE-1),PoolSwapTest.TestSettings(false,false),"");
        assertGt(buyZero?second.amount0():second.amount1(),0,"raw inventory should support the reverse trade");
    }

    function _dumpManager(string memory label) internal view {
        console2.log(label);
        console2.log(IERC20(Currency.unwrap(key.currency0)).balanceOf(address(manager)));
        console2.log(IERC20(Currency.unwrap(key.currency1)).balanceOf(address(manager)));
    }

    function _dumpVault(string memory label) internal view {
        // These slots come from forge inspect TickerDualPoolHookV1 storage-layout
        // at the pinned source commit: Inventory begins at slot 3.
        bytes32 partition = keccak256(abi.encode(key.toId(),address(usdc)));
        uint256 packed = uint256(vm.load(address(hook),keccak256(abi.encode(partition,uint256(5)))));
        uint256 poolShares = uint256(vm.load(address(hook),keccak256(abi.encode(partition,uint256(4)))));
        assertEq(poolShares,vault.balanceOf(address(hook)),"fixture has one pool using this wrapper");
        console2.log(label);
        console2.log(uint256(uint128(packed)));
        console2.log(packed >> 128);
        console2.log(poolShares);
        console2.log(vault.totalSupply());
        console2.log(usdc.balanceOf(address(vault)));
        console2.log(strategy.balanceOf(address(vault)));
        console2.log(strategy.convertToAssets(1e27));
        console2.log(vault.pendingYieldShares());
        console2.log(vault.convertToAssets(poolShares));
    }
    function test_exportLocalRPCState() public {
        if (!vm.envOr("EXPORT_KYBER_FIXTURE",false)) { vm.skip(true); return; }
        string memory allocation="{";
        for (uint256 i;i<exportAccounts.length;++i) {
            address account=exportAccounts[i];
            bytes32[] memory written=exportSlots[account];
            console2.log("exported storage slots",written.length);
            string memory storageJson="{";
            for(uint256 j;j<written.length;++j) {
                storageJson=string.concat(storageJson,j==0?"":",",'"',vm.toString(written[j]),'":"',vm.toString(vm.load(account,written[j])),'"');
            }
            storageJson=string.concat(storageJson,"}");
            allocation=string.concat(allocation,i==0?"":",",'"',vm.toString(account),'":{"code":"',vm.toString(account.code),'","nonce":',vm.toString(vm.getNonce(account)),',"balance":',vm.toString(account.balance),',"storage":',storageJson,"}");
        }
        vm.writeJson(string.concat(allocation,"}"),"/private/tmp/ticker-kyber-rpc-allocs.json");
        string memory object="fixture";
        vm.serializeAddress(object,"router",address(router));
        vm.serializeAddress(object,"payer",address(this));
        vm.serializeAddress(object,"hook",address(hook));
        vm.serializeAddress(object,"vault",address(vault));
        vm.serializeAddress(object,"strategy",address(strategy));
        vm.serializeAddress(object,"poolManager",address(manager));
        vm.serializeAddress(object,"stateView",0x77395F3b2E73aE90843717371294fa97cC419D64);
        vm.serializeAddress(object,"token0",Currency.unwrap(key.currency0));
        vm.serializeAddress(object,"token1",Currency.unwrap(key.currency1));
        vm.serializeString(object,"poolId",vm.toString(PoolId.unwrap(key.toId())));
        vm.serializeUint(object,"block",block.number);
        string memory manifest=vm.serializeUint(object,"timestamp",block.timestamp);
        vm.writeJson(manifest,"/private/tmp/ticker-kyber-rpc-manifest.json");
    }
}
