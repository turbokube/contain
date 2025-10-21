package appender

import (
    "regexp"
    "strings"
)

// fallbackPathValue is used when a PATH placeholder is referenced but the base image has no PATH.
const fallbackPathValue = "/usr/bin"

// substitutePlaceholders expands ${VAR} and $VAR occurrences in v using values from original.
// Order: first ${VAR}, then $VAR. No recursive expansion.
func substitutePlaceholders(v string, original map[string]string) string {
    if len(original) == 0 || v == "" || (!strings.Contains(v, "${") && !strings.Contains(v, "$")) {
        return v
    }
    for k, val := range original { // ${VAR}
        placeholder := "${" + k + "}"
        if strings.Contains(v, placeholder) {
            v = strings.ReplaceAll(v, placeholder, val)
        }
    }
    re := regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
    v = re.ReplaceAllStringFunc(v, func(m string) string {
        name := m[1:]
        if val, ok := original[name]; ok {
            return val
        }
        return m
    })
    return v
}

// applyEnvOverrides applies desired KEY=VALUE overrides (and additions) to existing environment slice.
// It performs placeholder expansion using the pre-override existing environment values and returns a new slice.
func applyEnvOverrides(existing []string, desiredKVs []string) []string {
    if len(desiredKVs) == 0 {
        // nothing to do
        out := make([]string, len(existing))
        copy(out, existing)
        return out
    }
    // Build desired map preserving order
    desired := make(map[string]string, len(desiredKVs))
    order := make([]string, 0, len(desiredKVs))
    for _, kv := range desiredKVs {
        if i := strings.Index(kv, "="); i > 0 {
            k := kv[:i]
            v := kv[i+1:]
            desired[k] = v
            order = append(order, k)
        }
    }
    // Snapshot original env key->value map for substitution
    original := make(map[string]string, len(existing))
    for _, e := range existing {
        if j := strings.Index(e, "="); j > 0 {
            original[e[:j]] = e[j+1:]
        }
    }
    // Copy existing slice to mutate
    out := make([]string, len(existing))
    copy(out, existing)
    seen := make(map[string]struct{}, len(desired))
    for i, e := range out {
        if j := strings.Index(e, "="); j > 0 {
            k := e[:j]
            if v, ok := desired[k]; ok {
                v = substitutePlaceholders(v, original)
                if strings.Contains(v, "${PATH}") { // Fallback when PATH missing
                    if _, has := original["PATH"]; !has {
                        v = strings.ReplaceAll(v, "${PATH}", fallbackPathValue)
                    }
                }
                out[i] = k + "=" + v
                seen[k] = struct{}{}
            }
        }
    }
    // Append new keys
    for _, k := range order {
        if _, ok := seen[k]; !ok {
            v := desired[k]
            v = substitutePlaceholders(v, original)
            if strings.Contains(v, "${PATH}") {
                if _, has := original["PATH"]; !has {
                    v = strings.ReplaceAll(v, "${PATH}", fallbackPathValue)
                }
            }
            out = append(out, k+"="+v)
        }
    }
    return out
}
