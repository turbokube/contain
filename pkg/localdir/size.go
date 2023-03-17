package localdir

import (
	"fmt"
	"strconv"
)

// NewSize should eventually support https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#meaning-of-memory
func NewSize(config string) (int64, error) {
	s, err := strconv.ParseInt(config, 10, 64)
	if err != nil {
		return s, fmt.Errorf("maxSize only supports numeric bytes syntax at the moment, got: %s", config)
	}
	return s, err
}
