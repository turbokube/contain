package v1

import (
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func amd64() v1.Platform { return v1.Platform{OS: "linux", Architecture: "amd64"} }
func arm64() v1.Platform { return v1.Platform{OS: "linux", Architecture: "arm64"} }
func arm64v8() v1.Platform {
	return v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}
}

func TestResolveLocalFilePath_ExactMatch(t *testing.T) {
	lf := LocalFile{
		Path: "fallback",
		PathPerPlatform: map[string]string{
			"linux/amd64": "amd64-bin",
			"linux/arm64": "arm64-bin",
		},
	}
	if got := ResolveLocalFilePath(lf, amd64()); got != "amd64-bin" {
		t.Errorf("amd64 got %q want amd64-bin", got)
	}
	if got := ResolveLocalFilePath(lf, arm64()); got != "arm64-bin" {
		t.Errorf("arm64 got %q want arm64-bin", got)
	}
}

func TestResolveLocalFilePath_VariantDroppedToOsArch(t *testing.T) {
	lf := LocalFile{
		PathPerPlatform: map[string]string{
			"linux/arm64": "arm64-bin",
		},
	}
	if got := ResolveLocalFilePath(lf, arm64v8()); got != "arm64-bin" {
		t.Errorf("variant fallback to os/arch got %q want arm64-bin", got)
	}
}

func TestResolveLocalFilePath_ExactVariantPreferredOverOsArch(t *testing.T) {
	lf := LocalFile{
		PathPerPlatform: map[string]string{
			"linux/arm64":    "arm64-generic",
			"linux/arm64/v8": "arm64-v8",
		},
	}
	if got := ResolveLocalFilePath(lf, arm64v8()); got != "arm64-v8" {
		t.Errorf("exact variant got %q want arm64-v8", got)
	}
}

func TestResolveLocalFilePath_FallbackToPath(t *testing.T) {
	lf := LocalFile{
		Path: "fallback",
		PathPerPlatform: map[string]string{
			"linux/amd64": "amd64-bin",
		},
	}
	if got := ResolveLocalFilePath(lf, arm64()); got != "fallback" {
		t.Errorf("arm64 got %q want fallback", got)
	}
}

func TestResolveLocalFilePath_EmptyWhenNothingConfigured(t *testing.T) {
	if got := ResolveLocalFilePath(LocalFile{}, amd64()); got != "" {
		t.Errorf("empty config got %q want empty", got)
	}
}

func TestResolveLocalFilePath_EmptyWhenNeitherMatches(t *testing.T) {
	lf := LocalFile{
		PathPerPlatform: map[string]string{"linux/amd64": "a"},
	}
	if got := ResolveLocalFilePath(lf, arm64()); got != "" {
		t.Errorf("no match got %q want empty", got)
	}
}

func TestResolveLocalFilePath_EmptyStringEntryTreatedAsMiss(t *testing.T) {
	lf := LocalFile{
		Path: "fallback",
		PathPerPlatform: map[string]string{
			"linux/arm64": "",
		},
	}
	if got := ResolveLocalFilePath(lf, arm64()); got != "fallback" {
		t.Errorf("empty map value should fall through to Path, got %q", got)
	}
}

func TestValidateLayers_OK(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{
		{LocalFile: LocalFile{PathPerPlatform: map[string]string{
			"linux/amd64": "a", "linux/arm64": "b",
		}}},
		{LocalDir: LocalDir{Path: "."}},
	}}
	if err := ValidateLayers(cfg, []v1.Platform{amd64(), arm64()}); err != nil {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestValidateLayers_MissingPlatform(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{
		{LocalFile: LocalFile{PathPerPlatform: map[string]string{"linux/amd64": "a"}}},
	}}
	err := ValidateLayers(cfg, []v1.Platform{amd64(), arm64()})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `layers[0].localFile`) {
		t.Errorf("error should name the offending layer path, got %q", msg)
	}
	if !strings.Contains(msg, "linux/arm64") {
		t.Errorf("error should name the missing platform, got %q", msg)
	}
	if !strings.Contains(msg, `pathPerPlatform["linux/arm64"]`) {
		t.Errorf("error should suggest the fix, got %q", msg)
	}
}

func TestValidateLayers_FallbackCoversMissingPlatform(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{
		{LocalFile: LocalFile{
			Path:            "fallback",
			PathPerPlatform: map[string]string{"linux/amd64": "a"},
		}},
	}}
	if err := ValidateLayers(cfg, []v1.Platform{amd64(), arm64()}); err != nil {
		t.Errorf("fallback should cover missing platform: %v", err)
	}
}

func TestValidateLayers_BothLocalFileAndLocalDir(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{
		{LocalFile: LocalFile{Path: "a"}, LocalDir: LocalDir{Path: "b"}},
	}}
	err := ValidateLayers(cfg, []v1.Platform{amd64()})
	if err == nil || !strings.Contains(err.Error(), "exactly one type") {
		t.Errorf("expected 'exactly one type' error, got %v", err)
	}
}

func TestValidateLayers_NeitherLocalFileNorLocalDir(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{{}}}
	err := ValidateLayers(cfg, []v1.Platform{amd64()})
	if err == nil || !strings.Contains(err.Error(), "no layer builder config found") {
		t.Errorf("expected 'no layer builder config found' error, got %v", err)
	}
}

func TestValidateLayers_InvalidPlatformKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"singlesegment", "linux"},
		{"trailing-slash", "linux/"},
		{"leading-slash", "/arm64"},
		{"four-segments", "linux/arm64/v8/extra"},
		{"whitespace", "linux /amd64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ContainConfig{Layers: []Layer{
				{LocalFile: LocalFile{
					Path:            "fallback",
					PathPerPlatform: map[string]string{tc.key: "x"},
				}},
			}}
			err := ValidateLayers(cfg, []v1.Platform{amd64()})
			if err == nil || !strings.Contains(err.Error(), "invalid key") {
				t.Errorf("expected invalid-key error for %q, got %v", tc.key, err)
			}
		})
	}
}

func TestValidateLayers_ValidVariantKeyAccepted(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{
		{LocalFile: LocalFile{PathPerPlatform: map[string]string{
			"linux/arm64/v8": "a",
		}}},
	}}
	if err := ValidateLayers(cfg, []v1.Platform{arm64v8()}); err != nil {
		t.Errorf("3-segment key should be accepted: %v", err)
	}
}

func TestValidateLayers_MultipleErrorsJoined(t *testing.T) {
	cfg := ContainConfig{Layers: []Layer{
		{LocalFile: LocalFile{PathPerPlatform: map[string]string{"linux/amd64": "a"}}},
		{LocalFile: LocalFile{PathPerPlatform: map[string]string{"linux/arm64": "b"}}},
	}}
	err := ValidateLayers(cfg, []v1.Platform{amd64(), arm64()})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "layers[0]") || !strings.Contains(err.Error(), "layers[1]") {
		t.Errorf("expected both offending layers in error, got %q", err.Error())
	}
}

func TestIsValidPlatformKey_Accepts(t *testing.T) {
	for _, k := range []string{"linux/amd64", "linux/arm64", "linux/arm64/v8", "windows/amd64"} {
		if !isValidPlatformKey(k) {
			t.Errorf("expected %q to be valid", k)
		}
	}
}

func TestIsValidPlatformKey_Rejects(t *testing.T) {
	for _, k := range []string{"", "linux", "linux/", "/amd64", "linux/amd64/v1/extra", "linux amd64", "linux\tamd64"} {
		if isValidPlatformKey(k) {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func arm() v1.Platform { return v1.Platform{OS: "linux", Architecture: "arm"} }
func armv7() v1.Platform {
	return v1.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}
}
func armv6() v1.Platform {
	return v1.Platform{OS: "linux", Architecture: "arm", Variant: "v6"}
}

// The mirror of TestResolveLocalFilePath_VariantDroppedToOsArch: a key
// spelled with the variant has to serve a base child spelled without it.
// Dropping the variant cannot do this, normalization can.
func TestResolveLocalFilePath_VariantKeyServesPlainPlatform(t *testing.T) {
	lf := LocalFile{
		PathPerPlatform: map[string]string{
			"linux/arm64/v8": "arm64-bin",
		},
	}
	if got := ResolveLocalFilePath(lf, arm64()); got != "arm64-bin" {
		t.Errorf("arm64 against an arm64/v8 key got %q want arm64-bin", got)
	}
}

// arm is the asymmetric case: the canonical variant is v7, not empty.
func TestResolveLocalFilePath_ArmNormalizesToV7(t *testing.T) {
	byPlain := LocalFile{PathPerPlatform: map[string]string{"linux/arm": "arm-bin"}}
	if got := ResolveLocalFilePath(byPlain, armv7()); got != "arm-bin" {
		t.Errorf("arm/v7 against a linux/arm key got %q want arm-bin", got)
	}
	byVariant := LocalFile{PathPerPlatform: map[string]string{"linux/arm/v7": "arm-bin"}}
	if got := ResolveLocalFilePath(byVariant, arm()); got != "arm-bin" {
		t.Errorf("arm against a linux/arm/v7 key got %q want arm-bin", got)
	}
	// arm/v6 is a different platform, so normalization does not pair it with
	// the linux/arm key. It still resolves, via the coarse os/arch fallback
	// that predates normalization: a key naming no variant is a deliberate
	// "this file serves the architecture". That is safe here in a way it
	// would not be for base selection, where matching two children of one
	// requested platform would publish an extra image.
	if got := ResolveLocalFilePath(byPlain, armv6()); got != "arm-bin" {
		t.Errorf("arm/v6 should fall back to the linux/arm key, got %q", got)
	}
	// but a variant-spelled key is specific and must not serve v6
	if got := ResolveLocalFilePath(byVariant, armv6()); got != "" {
		t.Errorf("arm/v6 against a linux/arm/v7 key got %q want empty", got)
	}
}

// Normalization must not make an unrelated key win, and the os/arch fallback
// still covers variants outside containerd's table.
func TestResolveLocalFilePath_NormalizationDoesNotWiden(t *testing.T) {
	lf := LocalFile{
		PathPerPlatform: map[string]string{
			"linux/arm64": "arm64-bin",
		},
	}
	if got := ResolveLocalFilePath(lf, v1.Platform{OS: "linux", Architecture: "arm", Variant: "v8"}); got != "" {
		t.Errorf("arm/v8 must not be served by an arm64 key, got %q", got)
	}
	amd := LocalFile{PathPerPlatform: map[string]string{"linux/amd64": "amd64-bin"}}
	// amd64/v3 is a distinct platform under normalization, but the os/arch
	// fallback still resolves it, as it did before normalization
	if got := ResolveLocalFilePath(amd, v1.Platform{OS: "linux", Architecture: "amd64", Variant: "v3"}); got != "amd64-bin" {
		t.Errorf("amd64/v3 should fall back to the linux/amd64 key, got %q", got)
	}
}

// An exact key always wins over a key that only matches after normalization.
func TestResolveLocalFilePath_ExactKeyWinsOverNormalized(t *testing.T) {
	lf := LocalFile{
		PathPerPlatform: map[string]string{
			"linux/arm64":    "arm64-generic",
			"linux/arm64/v8": "arm64-v8",
		},
	}
	if got := ResolveLocalFilePath(lf, arm64v8()); got != "arm64-v8" {
		t.Errorf("exact variant key got %q want arm64-v8", got)
	}
	if got := ResolveLocalFilePath(lf, arm64()); got != "arm64-generic" {
		t.Errorf("exact plain key got %q want arm64-generic", got)
	}
}
