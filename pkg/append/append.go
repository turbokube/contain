package append

import (
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// craneCmdAppend is basically https://github.com/google/go-containerregistry/blob/v0.13.0/cmd/crane/cmd/append.go
func craneCmdAppend(options *[]crane.Option) error {
	var base v1.Image
	var err error
	// paths
	var newLayers []string

	var baseMediaType = types.OCIManifestSchema1
	var baseConfigMediaType = types.OCIConfigJSON
	if baseRef == "" {
		logs.Warn.Printf("base unspecified, using empty image")
		base = empty.Image
		base = mutate.MediaType(base, baseMediaType)
		base = mutate.ConfigMediaType(base, baseConfigMediaType)
	} else {
		base, err = crane.Pull(baseRef, *options...)
		if err != nil {
			return fmt.Errorf("pulling %s: %w", baseRef, err)
		}
		baseMediaType, err = base.MediaType()
		if err != nil {
			return fmt.Errorf("getting base image media type: %w", err)
		}

	}

	layerType := types.DockerLayer

	if baseMediaType == types.OCIManifestSchema1 {
		layerType = types.OCILayer
	} else {
		// TODO not sure this is the case, and maybe we don't even need this
		baseConfigMediaType = types.DockerConfigJSON
	}

	nLayers := 1
	additions := make([]mutate.Addendum, 0, nLayers)

	additions[0] = mutate.Addendum{
		Layer: layer,
	}

	// we don't really need anything from https://github.com/google/go-containerregistry/blob/v0.13.0/pkg/crane/append.go#L42
	// except maybe
	img, err := crane.Append(base, newLayers...)
	if err != nil {
		return fmt.Errorf("appending %v: %w", newLayers, err)
	}

	if baseRef != "" && annotate {
		ref, err := name.ParseReference(baseRef)
		if err != nil {
			return fmt.Errorf("parsing ref %q: %w", baseRef, err)
		}

		baseDigest, err := base.Digest()
		if err != nil {
			return err
		}
		anns := map[string]string{
			specsv1.AnnotationBaseImageDigest: baseDigest.String(),
		}
		if _, ok := ref.(name.Tag); ok {
			anns[specsv1.AnnotationBaseImageName] = ref.Name()
		}
		img = mutate.Annotations(img, anns).(v1.Image)
	}

	if outFile != "" {
		if err := crane.Save(img, newTag, outFile); err != nil {
			return fmt.Errorf("writing output %q: %w", outFile, err)
		}
	} else {
		if err := crane.Push(img, newTag, *options...); err != nil {
			return fmt.Errorf("pushing image %s: %w", newTag, err)
		}
		ref, err := name.ParseReference(newTag)
		if err != nil {
			return fmt.Errorf("parsing reference %s: %w", newTag, err)
		}
		d, err := img.Digest()
		if err != nil {
			return fmt.Errorf("digest: %w", err)
		}
		fmt.Println(ref.Context().Digest(d.String()))
	}
}

func getLayer(path string, layerType types.MediaType) (v1.Layer, error) {
	f, err := streamFile(path)
	if err != nil {
		return nil, err
	}
	if f != nil {
		return stream.NewLayer(f, stream.WithMediaType(layerType)), nil
	}

	return tarball.LayerFromFile(path, tarball.WithMediaType(layerType))
}

// If we're dealing with a named pipe, trying to open it multiple times will
// fail, so we need to do a streaming upload.
//
// returns nil, nil for non-streaming files
func streamFile(path string) (*os.File, error) {
	if path == "-" {
		return os.Stdin, nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fi.Mode().IsRegular() {
		return os.Open(path)
	}

	return nil, nil
}
