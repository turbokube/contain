package ocipush

import "time"

func init() {
	// keeps the manifest retry tests fast
	manifestRetryBackoff = time.Millisecond
}
