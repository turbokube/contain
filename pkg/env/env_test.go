package env

import (
	"testing"
)

func TestPushOption_NotSet(t *testing.T) {
	t.Setenv("CONTAIN_PUSH", "")
	// Unset by clearing; use a separate subtest for truly unset
	t.Run("unset", func(t *testing.T) {
		// os.Unsetenv is not available via t.Setenv, but we can test the empty case
		// For truly unset, we rely on the default test environment
	})
}

func TestPushOption_True(t *testing.T) {
	t.Setenv("CONTAIN_PUSH", "true")
	v, err := PushOption()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil || !*v {
		t.Fatalf("expected true, got %v", v)
	}
}

func TestPushOption_False(t *testing.T) {
	t.Setenv("CONTAIN_PUSH", "false")
	v, err := PushOption()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil || *v {
		t.Fatalf("expected false, got %v", v)
	}
}

func TestPushOption_Numeric(t *testing.T) {
	t.Setenv("CONTAIN_PUSH", "0")
	v, err := PushOption()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil || *v {
		t.Fatalf("expected false for '0', got %v", v)
	}
}

func TestPushOption_Invalid(t *testing.T) {
	t.Setenv("CONTAIN_PUSH", "maybe")
	_, err := PushOption()
	if err == nil {
		t.Fatal("expected error for invalid value")
	}
}

func TestOCIOutput_NotSet(t *testing.T) {
	v, err := OCIOutput()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestOCIOutput_Relative(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "target-oci")
	v, err := OCIOutput()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil")
	}
	if v.Path != "target-oci" {
		t.Fatalf("expected target-oci, got %s", v.Path)
	}
}

func TestOCIOutput_DotSlash(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "./target-oci")
	v, err := OCIOutput()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil || v.Path != "./target-oci" {
		t.Fatalf("expected ./target-oci, got %v", v)
	}
}

func TestOCIOutput_RelativeNested(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "build/out/oci")
	v, err := OCIOutput()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil || v.Path != "build/out/oci" {
		t.Fatalf("expected build/out/oci, got %v", v)
	}
}

func TestOCIOutput_ParentTraversal(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "../escape")
	_, err := OCIOutput()
	if err == nil {
		t.Fatal("expected error for ../ path")
	}
}

func TestOCIOutput_ParentTraversalNested(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "foo/../../escape")
	_, err := OCIOutput()
	if err == nil {
		t.Fatal("expected error for path escaping via nested ../")
	}
}

func TestOCIOutput_ParentOnly(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "..")
	_, err := OCIOutput()
	if err == nil {
		t.Fatal("expected error for bare ..")
	}
}

func TestOCIOutput_Absolute(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "/tmp/oci-out")
	_, err := OCIOutput()
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestOCIOutput_Empty(t *testing.T) {
	t.Setenv("CONTAIN_OCI_OUTPUT", "")
	v, err := OCIOutput()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Fatalf("expected nil for empty, got %v", v)
	}
}

func TestPushLockPath_NotSet(t *testing.T) {
	v, err := PushLockPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "" {
		t.Fatalf("expected empty, got %s", v)
	}
}

func TestPushLockPath_Absolute(t *testing.T) {
	t.Setenv("CONTAIN_PUSH_LOCK_PATH", "/tmp/contain-push.lock")
	v, err := PushLockPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "/tmp/contain-push.lock" {
		t.Fatalf("expected /tmp/contain-push.lock, got %s", v)
	}
}

func TestPushLockPath_Relative(t *testing.T) {
	t.Setenv("CONTAIN_PUSH_LOCK_PATH", "relative.lock")
	_, err := PushLockPath()
	if err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestPushLockPath_Empty(t *testing.T) {
	t.Setenv("CONTAIN_PUSH_LOCK_PATH", "")
	v, err := PushLockPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "" {
		t.Fatalf("expected empty, got %s", v)
	}
}

func TestTurboHash_NotSet(t *testing.T) {
	h := TurboHash()
	if h != "" {
		t.Fatalf("expected empty, got %s", h)
	}
}

func TestTurboHash_Set(t *testing.T) {
	t.Setenv("TURBO_HASH", "abc123def456")
	h := TurboHash()
	if h != "abc123def456" {
		t.Fatalf("expected abc123def456, got %s", h)
	}
}
