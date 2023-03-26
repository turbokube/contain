package localdir

import (
	"fmt"
	"strconv"
)

// NewSize should eventually support https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#meaning-of-memory
func NewSize(config string) (int, error) {
	s, err := strconv.ParseInt(config, 10, 0)
	if err != nil {
		return 0, fmt.Errorf("maxSize only supports numeric bytes syntax at the moment, got: %s", config)
	}
	return int(s), err
}
