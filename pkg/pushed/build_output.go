package pushed

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/v1/types"
)

// BuildOutput is used to produce a similar output file that Skaffold does
type BuildOutput struct {
	// Skaffold is a superset of skaffold's --file-output format and can be used for skaffold deploy
	Skaffold *BuildOutputSkaffoldSuperset `json:"skaffold,omitempty"`
	// Buildctl matches buildctl's --metadata-file format
	Buildctl *MetadataSimilarToBuildctlFile `json:"buildctl,omitempty"`
	// Trace is internal metadata such as start/end and env; optional
	Trace *BuildTrace `json:"trace,omitempty"`
}

// Print writes the tag@digest for each built artifact, similar to previous contain package behavior.
func (b *BuildOutput) Print() {
	if b == nil || b.Skaffold == nil {
		return
	}
	for _, a := range b.Skaffold.Builds {
		fmt.Println(a.TagRef)
	}
}

type BuildOutputSkaffoldSuperset struct {
	Builds []Artifact `json:"builds"`
}

// Artifact returns the one artifact we built (the Skaffold format supports >=0)
func (b BuildOutput) Artifact() Artifact { return b.Skaffold.Builds[0] }

// NewBuildOutput constructs BuildOutput from a pushed Artifact (single-image or index).
// It preserves the JSON shape used by existing consumers. Buildctl metadata is populated
// from available details: config digest (if present) and platform only for single-image manifests.
func NewBuildOutput(tag string, a *Artifact) (*BuildOutput, error) {
	if a == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	// Skaffold section mirrors previous JSON shape
	s := &BuildOutputSkaffoldSuperset{Builds: []Artifact{*a}}

	// Buildctl metadata
	md := &MetadataSimilarToBuildctlFile{
		ContainerImageDigest: a.hash.String(),
		ImageName:            tag,
		ContainerImageDescriptor: ContainerImageDescriptor{
			MediaType: string(a.MediaType),
			Digest:    a.hash.String(),
			// Size omitted (unknown without fetching)
		},
	}
	// Only set platform when manifest is a single image (not index) and a platform is present
	if !isIndexMediaType(a.MediaType) && len(a.Platforms) > 0 {
		pf := a.Platforms[0]
		md.ContainerImageDescriptor.Platform = Platform{Architecture: pf.Architecture, OS: pf.OS}
	}
	// Config digest only for single images when we captured it
	if a.singleImageConfigHash.String() != "" {
		md.ContainerImageConfigDigest = a.singleImageConfigHash.String()
	}

	return &BuildOutput{Skaffold: s, Buildctl: md}, nil
}

// isIndexMediaType returns true if the media type denotes an image index/manifest list
func isIndexMediaType(mt types.MediaType) bool {
	switch mt {
	case types.OCIImageIndex, types.DockerManifestList:
		return true
	default:
		return false
	}
}

func (b *BuildOutput) WriteSkaffoldJSON(f *os.File) error {
	if b.Skaffold == nil {
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
