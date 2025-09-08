package appender

import v1 "github.com/google/go-containerregistry/pkg/v1"

// getLayersDelta returns metadata for appended layers
func (a *Appender) getLayersDelta(base *v1.Manifest, result *v1.Manifest) ([]AppendResultLayer, error) {
	baseLayers := len(base.Layers)
	var delta []AppendResultLayer
	for i, layer := range result.Layers {
		if i >= baseLayers {
			m := a.getManifestLayer(layer)
			delta = append(delta, m)
		}
	}
	return delta, nil
}

// getLayersDeltaForImages is a util for calling getLayersDelta with images
func (a *Appender) getLayersDeltaForImages(base v1.Image, result v1.Image) ([]AppendResultLayer, error) {
	baseManifest, err := base.Manifest()
	if err != nil {
		return nil, err
	}
	resultManifest, err := result.Manifest()
	if err != nil {
		return nil, err
	}
	return a.getLayersDelta(baseManifest, resultManifest)
}

// getManifestLayer removes unnecessary metadata
func (a *Appender) getManifestLayer(layer v1.Descriptor) AppendResultLayer {
	return AppendResultLayer{
		MediaType: layer.MediaType,
		Size:      layer.Size,
		Digest:    layer.Digest,
		Platform:  layer.Platform,
	}
}
