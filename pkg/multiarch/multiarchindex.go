package multiarch

import (
	"github.com/turbokube/contain/pkg/registry"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

type MultiArchIndex struct {
}

func (m *MultiArchIndex) GetPrototype() {
	// img, err := remote.Image(ref.Reference(), testCraneOptions.Remote...)
}

func (m *MultiArchIndex) WithPrototypeAppend() {

}

func (m *MultiArchIndex) PushIndex(registry *registry.RegistryConfig) error {
	return nil
}

func NewRequireMultiArchBase(config schema.ContainConfig, baseRegistry *registry.RegistryConfig) (*MultiArchIndex, error) {
	m := &MultiArchIndex{}
	return m, nil
}

func NewFromMultiArchBase(config schema.ContainConfig, baseRegistry *registry.RegistryConfig) (*MultiArchIndex, error) {
	m := &MultiArchIndex{}
	return m, nil
}
