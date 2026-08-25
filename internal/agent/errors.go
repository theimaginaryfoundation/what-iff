package agent

import "errors"

// ErrQuotaExceeded indicates that the turn was rejected because a usage limit was reached.
var ErrQuotaExceeded = errors.New("quota exceeded")
