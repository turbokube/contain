package contain

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
)

// BuildOutput is used to produce a similar output file that Skaffold does
type BuildOutput struct {
	// Skaffold matches skaffold's --file-output format and can be used for skaffold deploy
	Skaffold *BuildOutputSkaffold `json:"skaffold,omitempty"`
	// Buildctl matches buildctl's --metadata-file format
	Buildctl *MetadataSimilarToBuildctlFile `json:"buildctl,omitempty"`
	// Trace is internal, doesn't need to match the output of any other tool
	Trace *BuildTrace `json:"trace,omitempty"`
}

type BuildOutputSkaffold struct {
	Builds []Artifact `json:"builds"`
}

type Artifact struct {
	// Name without :tag or digest
	ImageName string `json:"imageName"`
	// Tag here includes name and digest, i.e. the config Tag to push to (use .Http.Tag for image tag)
	Tag string `json:"tag"`
	// reference is kept internally for reuse
	reference name.Reference
	// http is kept internally to assist http access
	hash v1.Hash
}

type ArtifactHttp struct {
	// Host is the registry host without protocol but with port
	Host string
	// Repository returns the path part of the image, excluding the /v2 http api prefix
	Repository string
	// Tag returns the tag name or "latest" if not specified
	Tag string
	// Hash returns digest, with algorithm and hex separable
	Hash v1.Hash
}

func (a *Artifact) Reference() name.Reference {
	return a.reference
}

func (a *Artifact) Http() ArtifactHttp {
	return ArtifactHttp{
		Host:       a.reference.Context().RegistryStr(),
		Repository: a.reference.Context().RepositoryStr(),
		Tag:        a.reference.Identifier(),
		Hash:       a.hash,
	}
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

	// found no way to get default repo and tag from
	r, err := name.ParseReference(tag)
	if err != nil {
		zap.L().Error("parse", zap.String("ref", tag))
		return nil, err
	}

	// actually we can't use ref because it prepends default registry, skaffold probably doesn't do that
	return &Artifact{
		Tag:       ref.String(),
		ImageName: named.Name(),
		reference: r,
		hash:      hash,
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
