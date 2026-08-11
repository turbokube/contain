package platform_test

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/platform"
)

func parse(t *testing.T, s string) v1.Platform {
	t.Helper()
	p, err := v1.ParsePlatform(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return *p
}

// The matrix asked for in the arch-support feature request. The arm
// asymmetry is the case a hand-rolled normalization gets backwards: for
// arm64 the canonical variant is empty, for arm it is v7.
func TestEqual(t *testing.T) {
	RegisterTestingT(t)
	same := [][2]string{
		{"linux/arm64", "linux/arm64/v8"},
		{"linux/arm64/v8", "linux/arm64"},
		{"linux/arm", "linux/arm/v7"},
		{"linux/arm/v7", "linux/arm"},
		{"linux/amd64", "linux/amd64/v1"},
		{"linux/amd64/v1", "linux/amd64"},
		// containerd also folds the alternate architecture spellings
		{"linux/arm64", "linux/aarch64"},
		{"linux/amd64", "linux/x86_64"},
		{"linux/arm/v7", "linux/armhf"},
		{"linux/arm/v6", "linux/armel"},
		// and is case insensitive
		{"linux/arm64", "Linux/ARM64/V8"},
	}
	for _, c := range same {
		Expect(platform.Equal(parse(t, c[0]), parse(t, c[1]))).
			To(BeTrue(), "%s should equal %s", c[0], c[1])
	}

	distinct := [][2]string{
		// a real feature level, not a spelling of plain arm64
		{"linux/arm64", "linux/arm64/v8.1"},
		{"linux/arm64", "linux/arm64/v9"},
		// arm variants are meaningfully different from each other
		{"linux/arm", "linux/arm/v6"},
		{"linux/arm/v6", "linux/arm/v7"},
		{"linux/arm/v7", "linux/arm/v8"},
		// amd64 microarchitecture levels above v1 are distinct
		{"linux/amd64", "linux/amd64/v2"},
		{"linux/amd64", "linux/amd64/v3"},
		{"linux/amd64", "linux/amd64/v4"},
		{"linux/amd64/v2", "linux/amd64/v3"},
		// normalization must not widen across architecture or os
		{"linux/arm64", "linux/arm/v8"},
		{"linux/amd64", "linux/386"},
		{"linux/arm64", "darwin/arm64"},
	}
	for _, c := range distinct {
		Expect(platform.Equal(parse(t, c[0]), parse(t, c[1]))).
			To(BeFalse(), "%s should not equal %s", c[0], c[1])
	}
}

// OSVersion and the feature lists are outside containerd's table, and
// v1.Platform.Equals still compares them, so they stay significant.
func TestEqualKeepsFieldsOutsideTheTable(t *testing.T) {
	RegisterTestingT(t)
	Expect(platform.Equal(
		v1.Platform{OS: "windows", Architecture: "amd64", OSVersion: "10.0.17763.1"},
		v1.Platform{OS: "windows", Architecture: "amd64", OSVersion: "10.0.20348.1"},
	)).To(BeFalse())
	Expect(platform.Equal(
		v1.Platform{OS: "linux", Architecture: "arm64", OSFeatures: []string{"a"}},
		v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"},
	)).To(BeFalse())
	Expect(platform.Equal(
		v1.Platform{OS: "linux", Architecture: "arm64", OSVersion: "1", OSFeatures: []string{"a", "b"}},
		v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8", OSVersion: "1", OSFeatures: []string{"b", "a"}},
	)).To(BeTrue())
}

// containerd's Normalize substitutes runtime.GOOS for an empty OS, which
// would make the same config mean different things on different runners.
func TestNormalizeLeavesEmptyOSEmpty(t *testing.T) {
	RegisterTestingT(t)
	Expect(platform.Normalize(v1.Platform{}).OS).To(Equal(""))
	Expect(platform.Normalize(v1.Platform{Architecture: "arm64", Variant: "v8"})).
		To(Equal(v1.Platform{Architecture: "arm64"}))
	// the zero platform is what the sync path passes to layer builders
	Expect(platform.Normalize(v1.Platform{})).To(Equal(v1.Platform{}))
}

func TestNormalizeCarriesUnrelatedFields(t *testing.T) {
	RegisterTestingT(t)
	got := platform.Normalize(v1.Platform{
		OS:           "linux",
		Architecture: "aarch64",
		Variant:      "v8",
		OSVersion:    "1.2.3",
		OSFeatures:   []string{"f1"},
		Features:     []string{"f2"},
	})
	Expect(got).To(Equal(v1.Platform{
		OS:           "linux",
		Architecture: "arm64",
		Variant:      "",
		OSVersion:    "1.2.3",
		OSFeatures:   []string{"f1"},
		Features:     []string{"f2"},
	}))
}

func TestString(t *testing.T) {
	RegisterTestingT(t)
	Expect(platform.String(nil)).To(Equal("<none>"))
	Expect(platform.String(&v1.Platform{})).To(Equal(""))
	Expect(platform.String(&v1.Platform{OS: "linux", Architecture: "arm64"})).To(Equal("linux/arm64"))
	Expect(platform.String(&v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"})).To(Equal("linux/arm64/v8"))
}
