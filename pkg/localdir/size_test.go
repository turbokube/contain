package localdir_test

import (
	"fmt"
	"testing"

	"github.com/turbokube/contain/pkg/localdir"
)

func TestParse(t *testing.T) {

	s, err := localdir.NewSize("123")
	if err != nil {
		t.Errorf("plain int %v", err)
	}
	if s != 123 {
		t.Errorf("plain int %d", s)
	}

	s, err = localdir.NewSize("123x")
	if err == nil {
		t.Errorf("should reject no bytes notation, got %d", s)
	}

	_, err = localdir.NewSize("100M")
	scopedout := fmt.Sprintf("%v", err)
	if scopedout != "maxSize only supports numeric bytes syntax at the moment, got: 100M" {
		t.Errorf("should clarify supported format, got: %s", scopedout)
	}

}
