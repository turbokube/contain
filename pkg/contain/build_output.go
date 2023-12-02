package contain

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/distribution/reference"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
)

// BuildOutput is used to produce a similar output file that Skaffold does
type BuildOutput struct {
	Builds []Artifact `json:"builds"`
}

type Artifact struct {
	// Name without :tag or digest
	ImageName string `json:"imageName"`
	// Tag here includes name and digest
	Tag string `json:"tag"`
}

// NewBuildOutput takes tag from config.Tag wich is name:tag and
// hash from for example append to produce build output for a single image.
func NewBuildOutput(tag string, hash v1.Hash) (*BuildOutput, error) {
	a, err := newArtifact(tag, hash)
	if err != nil {
		return nil, err
	}
	return &BuildOutput{
		Builds: []Artifact{*a},
	}, nil
}

func newArtifact(tag string, hash v1.Hash) (*Artifact, error) {
	full := fmt.Sprintf("%s@%v", tag, hash)

	ref, err := reference.Parse(full)
	if err != nil {
		zap.L().Error("parse", zap.String("ref", full), zap.Error(err))
		return nil, err
	}
	named := ref.(reference.Named)
	if named == nil {
		zap.L().Error("named", zap.String("parsed", full), zap.String("ref", ref.String()))
	}

	// actually we can't use ref because it prepends default registry, skaffold probably doesn't do that
	return &Artifact{
		Tag:       ref.String(),
		ImageName: named.Name(),
	}, nil
}

func (b *BuildOutput) Print() {
	for _, a := range b.Builds {
		fmt.Println(a.Tag)
	}
}

func (b *BuildOutput) WriteJSON(f *os.File) error {
	j, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = f.Write(j)
	if err != nil {
		return err
	}
	return nil
}
