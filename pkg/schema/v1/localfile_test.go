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
