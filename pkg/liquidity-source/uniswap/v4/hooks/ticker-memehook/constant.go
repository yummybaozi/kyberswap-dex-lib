package tickermemehook

import (
	"errors"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

// Exchange is the one place this source is named; rename here and in pkg/valueobject.
const Exchange = valueobject.ExchangeUniswapV4TickerMemeHook

// Monad mainnet proxy 0x223F8B1b43AEEc86d2659cCe36169f72f7FC2044 is deliberately
// not registered. Deployment recognition, service routing and gas validation
// are required before production activation. Tests register NewHook explicitly.

const basisPoints = 10_000

var ErrInvalidTerms = errors.New("ticker-memehook: hook fee plus creator tax is not below 100%")
