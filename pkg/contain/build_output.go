package contain

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/multiarch"
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
	// For multi-arch (index) images, neither skaffold nor buildctl writes platforms
	// each platform should be a
	Platforms []string `json:"platforms,omitempty"`
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

func toPlatforms(platforms []v1.Platform) []string {
	if len(platforms) == 0 {
		return nil // works with omitempty
	}
	result := make([]string, 0, len(platforms))
	for _, pf := range platforms {
		result = append(result, pf.String())
	}
	return result
}

// TODO now that the Pushed struct has been introduced we should be able to
// merge NewBuildOutput and NewBuildOutputWithMetadata + adapt things like descriptor.platform to mediaType

// NewBuildOutput takes tag from config.Tag wich is name:tag and
// hash from for example append to produce build output for a single image.
func NewBuildOutput(tag string, pushed multiarch.Pushed) (*BuildOutput, error) {
	a, err := newArtifact(tag, pushed.Digest)
	if err != nil {
		return nil, err
	}

	// Create metadata for buildctl format - we'll need more information later
	metadata := &MetadataSimilarToBuildctlFile{
		ContainerImageDigest: pushed.Digest.String(),
		ImageName:            tag,
		ContainerImageDescriptor: ContainerImageDescriptor{
			MediaType: string(pushed.MediaType),
			Digest:    pushed.Digest.String(),
			// Platform is singular and buildctl doesn't populate it for application/vnd.oci.image.manifest.v1+json
		},
	}

	return &BuildOutput{
		Skaffold: &BuildOutputSkaffold{
			Builds: []Artifact{*a},
		},
		Buildctl:  metadata,
		Platforms: toPlatforms(pushed.Platforms),
	}, nil
}

// NewBuildOutputWithMetadata creates BuildOutput with complete metadata from AppendResult
func NewBuildOutputWithMetadata(tag string, hash v1.Hash, image v1.Image, platform *v1.Platform) (*BuildOutput, error) {
	a, err := newArtifact(tag, hash)
	if err != nil {
		return nil, err
	}

	// Get the image config digest
	configHash, err := image.ConfigName()
	if err != nil {
		zap.L().Warn("failed to get config digest", zap.Error(err))
	}

	// Get the manifest for size and media type
	manifest, err := image.Manifest()
	if err != nil {
		zap.L().Warn("failed to get manifest", zap.Error(err))
	}

	// Create metadata for buildctl format
	metadata := &MetadataSimilarToBuildctlFile{
		ContainerImageDigest: hash.String(),
		ImageName:            tag,
	}

	// Set config digest if available
	if configHash.String() != "" {
		metadata.ContainerImageConfigDigest = configHash.String()
	}

	// Set descriptor information if manifest is available
	if manifest != nil {
		size, err := image.Size()
		if err != nil {
			zap.L().Warn("failed to get image size", zap.Error(err))
		}

		metadata.ContainerImageDescriptor = ContainerImageDescriptor{
			MediaType: string(manifest.MediaType),
			Digest:    hash.String(),
			Size:      int(size),
		}

		// Note that buildctl only writes platform for single-arch images
		if platform != nil {
			metadata.ContainerImageDescriptor.Platform = Platform{
				Architecture: platform.Architecture,
				OS:           platform.OS,
			}
		}
	}

	return &BuildOutput{
		Skaffold: &BuildOutputSkaffold{
			Builds: []Artifact{*a},
		},
		Buildctl: metadata,
		// not sure why platform is a pointer here
		Platforms: toPlatforms([]v1.Platform{*platform}),
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
	if b.Skaffold != nil {
		for _, a := range b.Skaffold.Builds {
			fmt.Println(a.Tag)
		}
	}
}

func (b *BuildOutput) WriteSkaffoldJSON(f *os.File) error {
	if b.Skaffold == nil {
		// If no Skaffold data, write empty builds array to maintain compatibility
		b.Skaffold = &BuildOutputSkaffold{Builds: []Artifact{}}
	}
	j, err := json.Marshal(b.Skaffold)
	if err != nil {
		return err
	}
	_, err = f.Write(j)
	return err
}

func (b *BuildOutput) WriteBuildctlJSON(f *os.File) error {
	if b.Buildctl == nil {
		// If no Buildctl data, write empty object to maintain compatibility
		b.Buildctl = &MetadataSimilarToBuildctlFile{}
	}
	j, err := json.Marshal(b.Buildctl)
	if err != nil {
		return err
	}
	_, err = f.Write(j)
	return err
}

func (b *BuildOutput) WriteJSON(f *os.File) error {
	j, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = f.Write(j)
	return err
}
