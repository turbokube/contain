package contain

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"go.uber.org/zap"
)

// OutputFormat matches crane's --format flag values.
type OutputFormat string

const (
	FormatTarball OutputFormat = "tarball"
	FormatOCI     OutputFormat = "oci"
)

func writeOutput(format OutputFormat, path string, ref name.Reference, img v1.Image, idx v1.ImageIndex) error {
	switch format {
	case FormatOCI:
		return writeOCI(path, ref, img, idx)
	case FormatTarball:
		return writeTarball(path, ref, img, idx)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}

func writeTarball(path string, ref name.Reference, img v1.Image, idx v1.ImageIndex) error {
	if idx != nil {
		return writeIndexTarball(path, ref, idx)
	}
	zap.L().Info("writing tarball", zap.String("path", path), zap.String("ref", ref.String()))
	return tarball.WriteToFile(path, ref, img)
}

func writeIndexTarball(path string, ref name.Reference, idx v1.ImageIndex) error {
	indexManifest, err := idx.IndexManifest()
	if err != nil {
		return fmt.Errorf("reading index manifest: %w", err)
	}
	refToImage := make(map[name.Reference]v1.Image, len(indexManifest.Manifests))
	for _, desc := range indexManifest.Manifests {
		img, err := idx.Image(desc.Digest)
		if err != nil {
			return fmt.Errorf("reading image %s from index: %w", desc.Digest, err)
		}
		refToImage[ref] = img
		break
	}
	if len(indexManifest.Manifests) > 1 {
		for _, desc := range indexManifest.Manifests[1:] {
			img, err := idx.Image(desc.Digest)
			if err != nil {
				return fmt.Errorf("reading image %s from index: %w", desc.Digest, err)
			}
			digestRef := ref.Context().Digest(desc.Digest.String())
			refToImage[digestRef] = img
		}
	}
	zap.L().Info("writing multi-arch tarball", zap.String("path", path), zap.Int("images", len(refToImage)))
	return tarball.MultiRefWriteToFile(path, refToImage)
}

func writeOCI(path string, ref name.Reference, img v1.Image, idx v1.ImageIndex) error {
	if idx != nil {
		return writeIndexOCI(path, ref, idx)
	}
	zap.L().Info("writing OCI layout", zap.String("path", path), zap.String("ref", ref.String()))
	lp, err := layout.Write(path, empty.Index)
	if err != nil {
		return fmt.Errorf("creating OCI layout at %s: %w", path, err)
	}
	return lp.AppendImage(img, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": ref.String(),
	}))
}

func writeIndexOCI(path string, ref name.Reference, idx v1.ImageIndex) error {
	zap.L().Info("writing OCI layout (index)", zap.String("path", path), zap.String("ref", ref.String()))
	lp, err := layout.Write(path, empty.Index)
	if err != nil {
		return fmt.Errorf("creating OCI layout at %s: %w", path, err)
	}
	return lp.AppendIndex(idx, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": ref.String(),
	}))
}
