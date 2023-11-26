package appender

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

const (
	progressReportMinInterval = "1s"
)

type Appender struct {
	config       *schema.ContainConfig
	baseEmpty    bool
	baseRef      name.Reference
	tagRef       name.Reference
	mediaType    types.MediaType
	layerType    types.MediaType
	craneOptions crane.Options
}

func New(config *schema.ContainConfig) (*Appender, error) {
	c := Appender{
		config:    config,
		baseEmpty: false,
	}
	var err error
	if config.Base == "" {
		c.baseEmpty = true
	} else {
		c.baseRef, err = name.ParseReference(config.Base)
		if err != nil {
			zap.L().Error("Failed to parse base", zap.String("ref", config.Base), zap.Error(err))
		}
		zap.L().Debug("base image", zap.String("ref", c.baseRef.String()))
	}
	c.tagRef, err = name.ParseReference(config.Tag)
	if err != nil {
		zap.L().Error("Failed to parse result image ref", zap.String("ref", config.Tag), zap.Error(err))
	}
	if c.tagRef != nil {
		zap.L().Debug("target image", zap.String("ref", c.tagRef.String()))
	}
	// https://github.com/google/go-containerregistry/blob/v0.13.0/pkg/crane/options.go#L43
	c.craneOptions = crane.Options{
		Remote: []remote.Option{
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
		},
		Keychain: authn.DefaultKeychain,
	}
	if strings.HasSuffix(".local", c.baseRef.Context().RegistryStr()) {
		zap.L().Debug("insecure access enabled", zap.String("registry", c.baseRef.Context().RegistryStr()))
		crane.Insecure(&c.craneOptions)
	} else if c.tagRef != nil && strings.HasSuffix(".local", c.tagRef.Context().RegistryStr()) {
		zap.L().Debug("insecure access enabled", zap.String("registry", c.tagRef.Context().RegistryStr()))
		crane.Insecure(&c.craneOptions)
	}
	return &c, nil
}

func (c *Appender) Options() *[]crane.Option {
	zap.L().Fatal("TODO how?")
	return nil
}

// base produces/retrieves the base image
// basically https://github.com/google/go-containerregistry/blob/v0.13.0/cmd/crane/cmd/append.go#L52
func (c *Appender) base() (v1.Image, error) {
	if c.mediaType != "" {
		zap.L().Fatal("contain.Base() has already been invoked")
	}
	var base v1.Image
	var err error
	var mediaType = types.OCIManifestSchema1
	var configType = types.OCIConfigJSON
	if c.baseEmpty {
		zap.L().Info("base unspecified, using empty image",
			zap.String("type", string(mediaType)),
			zap.String("configType", string(configType)),
		)
		base = empty.Image
		base = mutate.MediaType(base, mediaType)
		base = mutate.ConfigMediaType(base, configType)
	} else {
		base, err = remote.Image(c.baseRef, c.craneOptions.Remote...)
		if err != nil {
			return nil, fmt.Errorf("pulling %s: %w", c.baseRef.String(), err)
		}
		mediaType, err = base.MediaType()
		if err != nil {
			return nil, fmt.Errorf("getting base image media type: %w", err)
		}
	}

	// https://github.com/google/go-containerregistry/blob/v0.13.0/pkg/crane/append.go#L60
	if mediaType == types.OCIManifestSchema1 {
		c.layerType = types.OCILayer
	} else {
		c.layerType = types.DockerLayer
	}
	c.mediaType = mediaType

	return base, nil
}

// Append is what you call once layers are ready
func (c *Appender) Append(layers ...v1.Layer) (v1.Hash, error) {
	// Platform support remains to be verified with for example docker hub
	// See also https://github.com/google/go-containerregistry/issues/1456 and https://github.com/google/go-containerregistry/pull/1561
	if len(c.config.Platforms) > 1 {
		zap.L().Warn("unsupported multiple platforms, falling back to all", zap.Strings("platforms", c.config.Platforms))
	}
	if len(c.config.Platforms) == 1 {
		zap.L().Warn("unsupported single platform, falling back to all", zap.String("platform", c.config.Platforms[0]))
	}
	noresult := v1.Hash{}
	base, err := c.base()
	if err != nil {
		zap.L().Error("Failed to get base image", zap.Error(err))
		return noresult, err
	}
	baseDigest, err := base.Digest()
	if err != nil {
		zap.L().Error("Failed to get base image digest", zap.Error(err))
	}
	img, err := mutate.AppendLayers(base, layers...)
	if err != nil {
		zap.L().Error("Failed to append layers", zap.Error(err))
		return noresult, err
	}
	img = c.annotate(img, baseDigest)
	if err != nil {
		zap.L().Error("Failed to annotate", zap.Error(err))
		return noresult, err
	}
	imgDigest, err := img.Digest()
	if err != nil {
		zap.L().Error("Failed to get result image digest", zap.Error(err))
		return noresult, err
	}
	err = c.push(img)
	if err != nil {
		zap.L().Error("Failed to push", zap.Error(err))
		return noresult, err
	}
	zap.L().Info("pushed",
		zap.String("digest", imgDigest.String()),
	)
	return imgDigest, nil
}

// annotate is called after append
func (c *Appender) annotate(image v1.Image, baseDigest v1.Hash) v1.Image {
	// https://github.com/google/go-containerregistry/blob/v0.13.0/cmd/crane/cmd/append.go#L71
	a := map[string]string{
		specsv1.AnnotationBaseImageDigest: baseDigest.String(),
	}
	if _, ok := c.baseRef.(name.Tag); ok {
		a[specsv1.AnnotationBaseImageName] = c.baseRef.Name()
	}
	img := mutate.Annotations(image, a).(v1.Image)
	return img
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
		options := append(c.craneOptions.Remote, remote.WithProgress(progressChan))
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

func (c *Appender) LayerType() types.MediaType {
	if c.layerType == "" {
		zap.L().Fatal("Can not return media type before Base has been called")
	}
	return c.layerType
}
