package multiarch

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/match"
	"github.com/turbokube/contain/pkg/platform"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// match has utils for matching index member descriptors based on config

// ParseConfigPlatforms parses the platforms config into v1.Platform, keeping
// config order. Parsing stays with go-containerregistry rather than
// containerd's Parse, which infers os from runtime.GOOS for a bare
// architecture and so would read the same config differently per runner.
func ParseConfigPlatforms(configPlatforms []string) ([]v1.Platform, error) {
	platforms := make([]v1.Platform, len(configPlatforms))
	for i, c := range configPlatforms {
		p, err := v1.ParsePlatform(c)
		if err != nil {
			zap.L().Error("platform", zap.Int("i", i), zap.String("config", c), zap.Error(err))
			return nil, err
		}
		platforms[i] = *p
	}
	return platforms, nil
}

// UnmatchedPlatforms returns the entries of configPlatforms that none of
// matched denotes, in config order. It uses the same comparison as
// MatchPlatformsForAppend, so a config platform reported here is one that
// selected no manifest from the base index.
//
// An empty configPlatforms means "whatever the base has" and never reports
// anything.
func UnmatchedPlatforms(configPlatforms []string, matched []v1.Platform) ([]string, error) {
	if len(configPlatforms) == 0 {
		return nil, nil
	}
	wanted, err := ParseConfigPlatforms(configPlatforms)
	if err != nil {
		return nil, err
	}
	var unmatched []string
	for i, w := range wanted {
		found := false
		for _, m := range matched {
			if platform.Equal(m, w) {
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, configPlatforms[i])
		}
	}
	return unmatched, nil
}

// MatchPlatformsForAppend matches base index children against the platforms
// config, comparing normalized platforms rather than strings: linux/arm64 and
// linux/arm64/v8 are one platform, in both directions.
//
// This is still narrower than platform selection at runtime image pull. It
// does not match sub platforms the way containerd's Only does, so an arm64
// request never picks up an arm/v7 manifest, and it is not the
// unset-means-any rule that ko and crane use, under which a linux/arm request
// would select every arm variant in the base. One requested platform selects
// at most one base child.
func MatchPlatformsForAppend(config schema.ContainConfig) (match.Matcher, error) {
	if len(config.Platforms) == 0 {
		return func(desc v1.Descriptor) bool {
			return true
		}, nil
	}
	wanted, err := ParseConfigPlatforms(config.Platforms)
	if err != nil {
		return nil, err
	}
	return func(desc v1.Descriptor) bool {
		if desc.Platform == nil {
			return false
		}
		for _, w := range wanted {
			if platform.Equal(*desc.Platform, w) {
				return true
			}
		}
		return false
	}, nil
}
