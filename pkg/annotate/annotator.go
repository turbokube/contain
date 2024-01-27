package annotate

import "github.com/google/go-containerregistry/pkg/v1/partial"

// Annotator returns the same kind of manifest as it was given
// but updated with annotations
type Annotator func(partial.WithRawManifest) partial.WithRawManifest
