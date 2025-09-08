package pushed

// Platform, in buildctl metadata output, drops the "/v8" in linux/arm64/v8
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

type ContainerImageDescriptor struct {
	MediaType string   `json:"mediaType"`
	Digest    string   `json:"digest"`
	Size      int      `json:"size"`
	Platform  Platform `json:"platform"`
}

type MetadataSimilarToBuildctlFile struct {
	ContainerImageConfigDigest string                   `json:"containerimage.config.digest"`
	ContainerImageDescriptor   ContainerImageDescriptor `json:"containerimage.descriptor"`
	ContainerImageDigest       string                   `json:"containerimage.digest"`
	ImageName                  string                   `json:"image.name"`
	// TODO Annotations                map[string]string        `json:"annotations,omitempty"`
}
