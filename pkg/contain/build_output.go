package contain

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/turbokube/contain/pkg/pushed"
	"go.uber.org/zap"
)

// BuildOutput is used to produce a similar output file that Skaffold does
type BuildOutput struct {
	// Skaffold is a superset of skaffold's --file-output format and can be used for skaffold deploy
	Skaffold *BuildOutputSkaffoldSuperset `json:"skaffold,omitempty"`
	// Buildctl matches buildctl's --metadata-file format
	Buildctl *MetadataSimilarToBuildctlFile `json:"buildctl,omitempty"`
	// Trace is internal, doesn't need to match the output of any other tool
	Trace *pushed.BuildTrace `json:"trace,omitempty"`
}

type BuildOutputSkaffoldSuperset struct {
	Builds []Artifact `json:"builds"`
}

// Artifact returns the one artifact we built (the Skaffold format supports >=0)
func (b BuildOutput) Artifact() Artifact {
	return b.Skaffold.Builds[0]
}

type Artifact struct {
	// Name without :tag or digest
	ImageName string `json:"imageName"`
	// Tag here includes name and digest, i.e. the config Tag to push to (use .Http.Tag for image tag)
	Tag string `json:"tag"`
	// MediaType is not part of skaffold's build output format
	MediaType string `json:"mediaType"`
	// Platforms is not part of skaffold's build output format
	// But for multi-arch (index) images, neither skaffold nor buildctl writes platformsso we had to add it somewhere
	Platforms []string `json:"platforms"`
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

// NewBuildOutput takes tag from config.Tag (name:tag) and a pushed artifact to produce build output.
func NewBuildOutput(tag string, a *pushed.Artifact) (*BuildOutput, error) {
	if a == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	// Parse digest from artifact.TagRef
	var digestStr string
	if at := strings.LastIndex(a.TagRef, "@"); at != -1 {
		digestStr = a.TagRef[at+1:]
	}
	if digestStr == "" {
		return nil, fmt.Errorf("artifact.TagRef missing digest: %s", a.TagRef)
	}
	h, err := v1.NewHash(digestStr)
	if err != nil {
		return nil, err
	}

	ca, err := newArtifact(tag, h, string(a.MediaType), toPlatforms(a.Platforms))
	if err != nil {
		return nil, err
	}

	// Create metadata for buildctl format
	metadata := &MetadataSimilarToBuildctlFile{
		ContainerImageDigest: h.String(),
		ImageName:            tag,
		ContainerImageDescriptor: ContainerImageDescriptor{
			MediaType: string(a.MediaType),
			Digest:    h.String(),
		},
	}
	// Only set platform when not an index and when a platform is present
	if !isIndexMediaType(a.MediaType) && len(a.Platforms) > 0 {
		pf := a.Platforms[0]
		metadata.ContainerImageDescriptor.Platform = Platform{Architecture: pf.Architecture, OS: pf.OS}
	}
	// Config digest only for single images when available
	if cd := a.ConfigDigest(); cd != "" {
		metadata.ContainerImageConfigDigest = cd
	}

	return &BuildOutput{
		Skaffold: &BuildOutputSkaffoldSuperset{Builds: []Artifact{*ca}},
		Buildctl: metadata,
	}, nil
}

// isIndexMediaType returns true if the media type denotes an image index/manifest list.
func isIndexMediaType(mt types.MediaType) bool {
	switch mt {
	case types.OCIImageIndex, types.DockerManifestList:
		return true
	default:
		return false
	}
}

// NewBuildOutputWithMetadata creates BuildOutput with complete metadata from AppendResult
func NewBuildOutputWithMetadata(tag string, hash v1.Hash, image v1.Image, platform *v1.Platform) (*BuildOutput, error) {

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

	mediaType := string(manifest.MediaType)
	platforms := toPlatforms([]v1.Platform{*platform})

	a, err := newArtifact(tag, hash, mediaType, platforms)
	if err != nil {
		return nil, err
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
			MediaType: mediaType,
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
		Skaffold: &BuildOutputSkaffoldSuperset{
			Builds: []Artifact{*a},
		},
		Buildctl: metadata,
	}, nil
}

func newArtifact(tag string, hash v1.Hash, mediaType string, platforms []string) (*Artifact, error) {
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
		MediaType: mediaType,
		Platforms: platforms,
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
		b.Skaffold = &BuildOutputSkaffoldSuperset{Builds: []Artifact{}}
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
