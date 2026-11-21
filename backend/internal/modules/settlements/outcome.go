package settlements

import (
	"errors"
	"strings"
)

// A gateway reports the result of Transfer with one of two kinds of error, and
// the difference decides whether money may be sent again.
var (
	// ErrTransferRejected means the gateway definitely refused the transfer
	// (insufficient balance, invalid account, bad credentials). Nothing was
	// sent, so the settlements are freed and retried in a later run.
	ErrTransferRejected = errors.New("payout transfer rejected")
	// ErrTransferUncertain means the outcome is unknown (timeout, 5xx, dropped
	// connection): the payout may or may not have left. It must NOT be freed or
	// re-created; it is re-sent with the same idempotency key, which the
	// gateway de-duplicates, or confirmed by status.
	ErrTransferUncertain = errors.New("payout transfer outcome unknown")
)

// Outcome is the final meaning of a gateway payout status.
type Outcome int

const (
	// OutcomePending: still moving through the banks; check again later.
	OutcomePending Outcome = iota
	// OutcomePaid: the money reached the owner's bank.
	OutcomePaid
	// OutcomeFailed: the payout did not complete and the money is not with the owner.
	OutcomeFailed
)

// ClassifyStatus maps a gateway payout status to an Outcome. Unknown statuses
// stay pending: a payout is never paid on a status we do not recognise.
func ClassifyStatus(status string) Outcome {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "processed":
		return OutcomePaid
	case "failed", "rejected", "cancelled", "canceled", "reversed":
		return OutcomeFailed
	default: // queued, pending, processing, scheduled, unknown
		return OutcomePending
	}
}
