package tickerdualpool

import "github.com/ethereum/go-ethereum/common"

// Pin the live Monad hook deployment, including compiler metadata and immutables.
// An implementation change must be reviewed before updating these pins.
var tickerHookImplementation = common.HexToAddress("0x844a14bae1b67df6064bb87ba5ff941dc02649e0")
var tickerHookCodeHash = common.HexToHash("0xc812006c87f88e3056cc31a5bb39a4b64fa011a23c718923f29f949f53b55eb0")
var tickerProxyCodeHash = common.HexToHash("0x824a513357c89e28b2b23a5d727a23bdb3b69db732883d999165af4e064e3120")

// Vault constructor settings vary by launch; retain the existing accounting-code check.
var tickerVaultFingerprint = runtimeFingerprint{Size: 12044, Hash: common.HexToHash("0xc34334e77e1c8bb3556719f884249d7c41f926a0469b850052fc29dde3fb53b7"), ImmutableOffsets: []int{503, 892, 1527, 2426, 2933, 3803, 4210, 4486, 5566, 5846, 6421, 6577, 6646, 7524, 7602, 7931, 8447, 9257, 9510, 9772, 9953, 10146, 10375, 10840, 11119}}
