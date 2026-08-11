package multiarch

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/match"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// match has utils for matching index member descriptors based on config

// PlatformString renders a platform for logs and errors, tolerating nil.
// v1.Platform.String has a value receiver, so calling it on a nil
// *v1.Platform panics, and platform is OPTIONAL on an index descriptor:
// a base index may carry manifests (referrers, artifacts) without one.
func PlatformString(p *v1.Platform) string {
	if p == nil {
		return "<none>"
	}
	return p.String()
}

// UnmatchedPlatforms returns the entries of configPlatforms that none of
// matched is equal to, in config order. It uses the same equality as
// MatchPlatformsForAppend, so a config platform reported here is one that
// selected no manifest from the base index.
//
// An empty configPlatforms means "whatever the base has" and never reports
// anything.
func UnmatchedPlatforms(configPlatforms []string, matched []v1.Platform) ([]string, error) {
	if len(configPlatforms) == 0 {
		return nil, nil
	}
	var unmatched []string
	for i, c := range configPlatforms {
		p, err := v1.ParsePlatform(c)
		if err != nil {
			zap.L().Error("platform", zap.Int("i", i), zap.String("config", c), zap.Error(err))
			return nil, err
		}
		found := false
		for _, m := range matched {
			if m.Equals(*p) {
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, c)
		}
	}
	return unmatched, nil
}

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
