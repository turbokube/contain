// Package platform normalizes OCI platform values, so that the different
// spellings the ecosystem uses for one platform compare equal.
//
// linux/arm64 and linux/arm64/v8 are the same platform: the image spec makes
// variant OPTIONAL and gives omitting it no distinct meaning. Which spelling
// is canonical has changed over time (containerd v1.2-v1.4 filled in v8 where
// the current code empties it), so comparing the strings is only ever correct
// against bases that happen to share the comparer's assumption.
//
// Normalization is not the same as widening. Every genuinely distinct pair
// stays distinct: arm/v6 against arm/v7, the arm64 feature levels v8.1 and v9,
// and the amd64 microarchitecture levels v2 to v4. That is why containerd
// canonicalizes rather than picking a winner, and why this package uses
// containerd's table rather than a local one: the rules are asymmetric per
// architecture (arm64 drops an empty-equivalent variant, arm adds one) and a
// hand-rolled version gets arm backwards.
package platform

import (
	"github.com/containerd/platforms"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

// Normalize returns p with os, architecture and variant in containerd's
// canonical form. OSVersion, OSFeatures and Features are carried through
// untouched; containerd's table does not cover them.
//
// An empty OS stays empty. containerd substitutes runtime.GOOS there, which
// would make an under-specified platform mean one thing on a linux runner and
// another on a developer's mac.
func Normalize(p v1.Platform) v1.Platform {
	n := platforms.Normalize(specs.Platform{
		OS:           p.OS,
		Architecture: p.Architecture,
		Variant:      p.Variant,
	})
	if p.OS != "" {
		p.OS = n.OS
	}
	p.Architecture = n.Architecture
	p.Variant = n.Variant
	return p
}

// Equal reports whether a and b denote the same platform once normalized.
// It is symmetric, unlike v1.Platform.Satisfies, and it does not match sub
// platforms the way containerd's Only does: an arm64 request stays an arm64
// request and never picks up an arm/v7 manifest.
func Equal(a, b v1.Platform) bool {
	return Normalize(a).Equals(Normalize(b))
}

// String renders a platform for logs and errors, tolerating nil.
// v1.Platform.String has a value receiver, so calling it on a nil
// *v1.Platform panics, and platform is OPTIONAL on an index descriptor: a
// base index may carry manifests (referrers, artifacts) without one.
func String(p *v1.Platform) string {
	if p == nil {
		return "<none>"
	}
	return p.String()
}
