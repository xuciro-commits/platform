package host

import "platformserver/platform"

type TransactionHost interface {
	CheckTxnCall(platform.TxnCall) error
}
