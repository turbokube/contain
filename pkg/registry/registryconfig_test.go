package registry_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/registry"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

func TestLocal(t *testing.T) {
	RegisterTestingT(t)

	c1, err := registry.New(schema.ContainConfig{
		Base: "registry.local/my/img",
	})
	Expect(err).To(BeNil())
	Expect(fmt.Sprintf("%v", c1)).To(ContainSubstring("true 0 false"))
	c2, err := registry.New(schema.ContainConfig{
		Base: "registry.example.net/my/img",
	})
	Expect(err).To(BeNil())
	Expect(fmt.Sprintf("%v", c2)).To(ContainSubstring("false 0 false"))

}
