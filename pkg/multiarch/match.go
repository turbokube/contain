package multiarch

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/match"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// match has utils for matching index member descriptors based on config

// MatchPlatformsForAppend matches only on platform equality
// which is stricter than platform at runtime image pull because
// we don't want to widen the scope of a base image
// (config could allow different matching as opt-in, maybe with v1.Platform.Satisfies)
func MatchPlatformsForAppend(config schema.ContainConfig) (match.Matcher, error) {
	count := len(config.Platforms)
	if count == 0 {
		return func(desc v1.Descriptor) bool {
			return true
		}, nil
	}
	platforms := make([]v1.Platform, len(config.Platforms))
	for i, c := range config.Platforms {
		p, err := v1.ParsePlatform(c)
		if err != nil {
			zap.L().Error("platform", zap.Int("i", i), zap.String("config", c), zap.Error(err))
			return nil, err
		}
		platforms[i] = *p
	}
	return match.Platforms(platforms...), nil
}
