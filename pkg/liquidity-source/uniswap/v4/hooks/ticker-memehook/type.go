package tickermemehook

import "github.com/ethereum/go-ethereum/common"

// launchInfo mirrors TickerMemeHook.LaunchInfo, the output of launches(poolId).
// Only registered, hookFeeBps and creatorTaxBps price a swap; the rest governs
// the fee sweep. abi.Unpack fills the struct positionally, so every output stays.
type launchInfo struct {
	Registered                bool
	MemecoinIsCurrency0       bool
	Memecoin                  common.Address
	QuoteToken                common.Address
	Creator                   common.Address
	BuybackCreatorRecipient   common.Address
	ProtocolFeeRecipient      common.Address
	CreatorTaxBps             uint16
	ProtocolFeeShareBps       uint16
	BuybackBurnBps            uint16
	HookFeeBps                uint16
	MaxInternalPriceImpactBps uint16
	BuybackEnabled            bool
}
