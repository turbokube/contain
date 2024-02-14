// Package appender provides an API to push layers,
// report progress while pushing and return a resulting image+hash
package appender

import (
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/turbokube/contain/pkg/annotate"
	"github.com/turbokube/contain/pkg/registry"
	"go.uber.org/zap"
)

const (
	progressReportMinInterval = "1s"
)

// Appender transfers layers AND pushes manifest
// and is reasonably efficient should multiple appenders push the same layers
type Appender struct {
	baseRef    name.Digest
	baseConfig *registry.RegistryConfig
	tagRef     name.Reference
	annotators []annotate.Annotator
}

type AppendAnnotate func(partial.WithRawManifest) v1.Image

// AppendResultLayer is the part of go-containerregistry/pkg/v1
type AppendResultLayer struct {
	MediaType types.MediaType `json:"mediaType"`
	Size      int64           `json:"size"`
	Digest    v1.Hash         `json:"digest"`
}

// // Assert that Descriptor implements AppendResultLayer.
// // like https://github.com/ko-build/ko/blob/v0.15.1/pkg/build/build.go#L55
// var _ AppendResultLayer = (v1.Descriptor)(AppendResultLayer{})

type AppendResult struct {
	// Hash is the digest of the pushed manifest, including annotate
	Hash v1.Hash
	// Pushed is how this result can be added to an index manifest
	Pushed mutate.IndexAddendum
	// AddedManifestLayers are manifest data for appended and pushed layers
	AddedManifestLayers []AppendResultLayer
}

var AppendResultNone = AppendResult{
	Hash:                v1.Hash{},
	Pushed:              mutate.IndexAddendum{},
	AddedManifestLayers: []AppendResultLayer{},
}

// New starts Appender setup from a fully specified base ref
// which might be an image in an index, not necessarily Contain's base.
// This base image has typically not been read yet, not even its manifest.
// Remember to optionally call WithAnnotate before Append.
func New(baseRef name.Digest, baseConfig *registry.RegistryConfig, tagRef name.Reference) (*Appender, error) {
	c := Appender{
		baseRef:    baseRef,
		baseConfig: baseConfig,
		tagRef:     tagRef,
	}
	// var err error

	// c.baseRef, err = name.ParseReference(config.Base)
	// fullRef := c.baseRef.(name.Digest)
	// if err != nil {
	// 	zap.L().Error("Failed to parse base", zap.String("ref", config.Base), zap.Error(err))
	// }
	// zap.L().Debug("base image", zap.String("ref", c.baseRef.String()))

	// c.tagRef, err = name.ParseReference(config.Tag)
	// if err != nil {
	// 	zap.L().Error("Failed to parse result image ref", zap.String("ref", config.Tag), zap.Error(err))
	// }
	// if c.tagRef != nil {
	// 	zap.L().Debug("target image", zap.String("ref", c.tagRef.String()))
	// }

	return &c, nil
}

func (c *Appender) WithAnnotate(annotate annotate.Annotator) {
	c.annotators = append(c.annotators, annotate)
}

func (c *Appender) getPushConfig() *registry.RegistryConfig {
	return c.baseConfig
}

// base produces/retrieves the base image
// basically https://github.com/google/go-containerregistry/blob/v0.13.0/cmd/crane/cmd/append.go#L52
func (c *Appender) base() (v1.Image, error) {

	base, err := remote.Image(c.baseRef, c.baseConfig.CraneOptions.Remote...)
	if err != nil {
		return nil, fmt.Errorf("pulling %s: %w", c.baseRef.String(), err)
	}
	mediaType, err := base.MediaType()
	if err != nil {
		return nil, fmt.Errorf("getting base image media type: %w", err)
	}
	// When starting with an ImageIndex this should not need to happen because all mediaTypes can be validated from the index manifest
	if mediaType != types.OCIManifestSchema1 {
		return nil, fmt.Errorf("currently non-OCI manifests are de-supported, got: %s", mediaType)
	}

	return base, nil
}

// Append is what you call once layers are ready
func (c *Appender) Append(layers ...v1.Layer) (AppendResult, error) {

	base, err := c.base()
	if err != nil {
		zap.L().Error("Failed to get base image", zap.Error(err))
		return AppendResultNone, err
	}
	baseConfig, err := base.ConfigFile()
	if err != nil {
		zap.L().Error("get base image config", zap.Error(err))
		return AppendResultNone, err
	}

	img, err := mutate.AppendLayers(base, layers...)
	if err != nil {
		zap.L().Error("Failed to append layers", zap.Error(err))
		return AppendResultNone, err
	}
	for _, annotate := range c.annotators {
		img = annotate(img).(v1.Image)
	}
	imgDigest, err := img.Digest()
	if err != nil {
		zap.L().Error("Failed to get result image digest", zap.Error(err))
		return AppendResultNone, err
	}
	err = c.push(img)
	if err != nil {
		zap.L().Error("Failed to push", zap.Error(err))
		return AppendResultNone, err
	}
	zap.L().Info("pushed",
		zap.String("digest", imgDigest.String()),
	)
	delta, err := c.getLayersDeltaForImages(base, img)
	if err != nil {
		zap.L().Error("layers delta", zap.Error(err))
		return AppendResultNone, err
	}
	appendable := mutate.IndexAddendum{
		Add: img,
		Descriptor: v1.Descriptor{
			Platform: baseConfig.Platform(),
		},
	}
	result := AppendResult{
		Hash:                imgDigest,
		Pushed:              appendable,
		AddedManifestLayers: delta,
	}
	return result, nil
}

func (c *Appender) push(image v1.Image) error {
	mediaType, err := image.MediaType()
	if err != nil {
		return err
	}
	zap.L().Info("pushing", zap.String("mediaType", string(mediaType)))

	debounce, err := time.ParseDuration(progressReportMinInterval)
	if err != nil {
		zap.L().Fatal("failed to parse debounce interval", zap.String("value", progressReportMinInterval), zap.Error(err))
	}

	progressChan := make(chan v1.Update, 200)
	errChan := make(chan error, 2)

	go func() {
		options := append(c.getPushConfig().CraneOptions.Remote, remote.WithProgress(progressChan))
		errChan <- remote.Write(
			c.tagRef,
			image,
			options...,
		)
	}()

	logger := zap.L()
	nextProgress := time.Now().Add(debounce)

	for update := range progressChan {
		if update.Error != nil {
			logger.Error("push update", zap.Error(update.Error))
			errChan <- update.Error
			break
		}

		if update.Complete == update.Total {
			logger.Info("pushed", zap.Int64("completed", update.Complete), zap.Int64("total", update.Total))
		} else {
			if time.Now().After(nextProgress) {
				nextProgress = time.Now().Add(debounce)
				logger.Info("push", zap.Int64("completed", update.Complete), zap.Int64("total", update.Total))
			}
		}
	}

	return <-errChan
}
