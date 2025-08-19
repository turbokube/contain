package annotate

import (
	"fmt"
	"os"
	"strings"

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

// NewBaseImageAnnotations returns an annotator that sets the standard base image
// annotations used by crane rebase (specsv1.AnnotationBaseImageDigest/Name).
// It accepts a config.Base string that MUST include a digest (contain currently
// requires this) and MAY include a tag ("tagged digest"). We always set the digest
// annotation. For the name annotation we reconstruct the reference before the '@'
// if present so that tagged digests produce a tag reference and pure digests are
// omitted (mirrors crane append behavior that only sets name when a tag ref was used).
func NewBaseImageAnnotations(base string) (Annotator, error) {
	// Parse to validate and extract digest
	ref, err := name.ParseReference(base)
	if err != nil {
		return nil, err
	}
	d, ok := ref.(name.Digest)
	if !ok {
		return nil, fmt.Errorf("base reference did not parse as digest: %s", base)
	}
	digestStr := d.DigestStr()
	// Attempt to extract full reference (including registry host) prior to '@'
	var baseName string
	if at := strings.Index(base, "@"); at > 0 {
		baseName = base[:at]
	}
	// Optional host override for test determinism or special cases.
	if override := os.Getenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE"); override != "" && baseName != "" {
		if slash := strings.Index(baseName, "/"); slash > 0 {
			baseName = override + baseName[slash:]
		}
	}
	return func(image partial.WithRawManifest) partial.WithRawManifest {
		anns := map[string]string{
			specsv1.AnnotationBaseImageDigest: digestStr, // digestStr already includes algorithm
		}
		if baseName != "" {
			anns[specsv1.AnnotationBaseImageName] = baseName
		}
		return mutate.Annotations(image, anns)
	}, nil
}
