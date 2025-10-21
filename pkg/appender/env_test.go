package appender

import (
    "testing"
    "strings"
)

func TestSubstitutePlaceholders(t *testing.T) {
    original := map[string]string{"PATH": "/bin", "FOO": "bar"}
    cases := []struct{ in, expect string }{
        {"${PATH}:/opt", "/bin:/opt"},
        {"$PATH:/opt", "/bin:/opt"},
        {"${PATH}FOO", "/binFOO"},
        {"$FOO-sfx", "bar-sfx"},
        {"nochange", "nochange"},
    }
    for i, c := range cases {
        got := substitutePlaceholders(c.in, original)
        if got != c.expect {
            t.Fatalf("case %d expected %q got %q", i, c.expect, got)
        }
    }
}

func TestApplyEnvOverridesBasic(t *testing.T) {
    existing := []string{"PATH=/bin", "FOO=orig", "KEEP=1"}
    desired := []string{"FOO=bar", "NEW=val"}
    out := applyEnvOverrides(existing, desired)
    joined := strings.Join(out, "|")
    if !strings.Contains(joined, "FOO=bar") {
        t.Fatalf("expected override FOO=bar got %v", out)
    }
    if !strings.Contains(joined, "NEW=val") {
        t.Fatalf("expected append NEW=val got %v", out)
    }
    if !strings.Contains(joined, "KEEP=1") {
        t.Fatalf("expected keep KEEP=1 got %v", out)
    }
}

func TestApplyEnvOverridesExpansion(t *testing.T) {
    existing := []string{"PATH=/bin"}
    desired := []string{"PATH=${PATH}:/usr/local/bin"}
    out := applyEnvOverrides(existing, desired)
    var pathVal string
    for _, e := range out {
        if strings.HasPrefix(e, "PATH=") { pathVal = e }
    }
    if pathVal != "PATH=/bin:/usr/local/bin" {
        t.Fatalf("expected expanded PATH got %s", pathVal)
    }
}

func TestApplyEnvOverridesFallbackPath(t *testing.T) {
    existing := []string{"FOO=bar"} // no PATH present
    desired := []string{"PATH=${PATH}:/x"}
    out := applyEnvOverrides(existing, desired)
    var pathVal string
    for _, e := range out { if strings.HasPrefix(e, "PATH=") { pathVal = e } }
    if pathVal != "PATH=/usr/bin:/x" { // fallbackPathValue
        t.Fatalf("expected fallback PATH expansion got %s", pathVal)
    }
}

func TestApplyEnvOverridesDollarVar(t *testing.T) {
    existing := []string{"PATH=/bin"}
    desired := []string{"PATH=$PATH:/opt"}
    out := applyEnvOverrides(existing, desired)
    var pathVal string
    for _, e := range out { if strings.HasPrefix(e, "PATH=") { pathVal = e } }
    if pathVal != "PATH=/bin:/opt" {
        t.Fatalf("expected $PATH expansion got %s", pathVal)
    }
}
