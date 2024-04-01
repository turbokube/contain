package annotate

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// NewBaseref returns an annotator that records original base
// i.e. the given base image that might be an index, not necessarily append's base
func NewBaseref(baseRef name.Tag, baseDigest v1.Hash) (Annotator, error) {
	return func(image partial.WithRawManifest) partial.WithRawManifest {
		// https://github.com/google/go-containerregistry/blob/v0.13.0/cmd/crane/cmd/append.go#L71
		a := map[string]string{
			specsv1.AnnotationBaseImageDigest: baseDigest.String(),
			specsv1.AnnotationBaseImageName: fmt.Sprintf("/%s:%s",
				baseRef.Context().RepositoryStr(),
				baseRef.Identifier(),
			),
		}
		return mutate.Annotations(image, a)
	}, nil
}
