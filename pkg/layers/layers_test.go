package layers

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

// layerFiles returns a name->contents map extracted from the layer's
// uncompressed tar stream. Used to assert the file content produced for
// each platform.
func layerFiles(t *testing.T, layer v1.Layer) map[string]string {
	t.Helper()
	rc, err := layer.Uncompressed()
	if err != nil {
		t.Fatalf("uncompressed: %v", err)
	}
	defer rc.Close()
	out := map[string]string{}
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		buf := new(strings.Builder)
		if _, err := io.Copy(buf, tr); err != nil {
			t.Fatalf("tar copy: %v", err)
		}
		out[hdr.Name] = buf.String()
	}
	return out
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func amd64() v1.Platform { return v1.Platform{OS: "linux", Architecture: "amd64"} }
func arm64() v1.Platform { return v1.Platform{OS: "linux", Architecture: "arm64"} }

func TestNewLayerBuilder_LocalFilePathPerPlatform(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "amd64.bin", "AMD")
	writeFile(t, dir, "arm64.bin", "ARM")

	cfg := schema.Layer{LocalFile: schema.LocalFile{
		PathPerPlatform: map[string]string{
			"linux/amd64": filepath.Join(dir, "amd64.bin"),
			"linux/arm64": filepath.Join(dir, "arm64.bin"),
		},
		ContainerPath: "/bin/mybinary",
	}}
	b, err := NewLayerBuilder(cfg)
	if err != nil {
		t.Fatalf("NewLayerBuilder: %v", err)
	}

	amdLayer, err := b(amd64())
	if err != nil {
		t.Fatalf("amd64 build: %v", err)
	}
	armLayer, err := b(arm64())
	if err != nil {
		t.Fatalf("arm64 build: %v", err)
	}

	amdFiles := layerFiles(t, amdLayer)
	armFiles := layerFiles(t, armLayer)
	if got := amdFiles["/bin/mybinary"]; got != "AMD" {
		t.Errorf("amd64 file got %q, want AMD (files: %v)", got, amdFiles)
	}
	if got := armFiles["/bin/mybinary"]; got != "ARM" {
		t.Errorf("arm64 file got %q, want ARM (files: %v)", got, armFiles)
	}

	amdDigest, _ := amdLayer.Digest()
	armDigest, _ := armLayer.Digest()
	if amdDigest == armDigest {
		t.Errorf("per-arch layers must have distinct digests, both %s", amdDigest)
	}
}

func TestNewLayerBuilder_FallbackPathUsedWhenPlatformMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fallback.bin", "FALLBACK")
	writeFile(t, dir, "amd64.bin", "AMD")

	cfg := schema.Layer{LocalFile: schema.LocalFile{
		Path: filepath.Join(dir, "fallback.bin"),
		PathPerPlatform: map[string]string{
			"linux/amd64": filepath.Join(dir, "amd64.bin"),
		},
		ContainerPath: "/bin/x",
	}}
	b, err := NewLayerBuilder(cfg)
	if err != nil {
		t.Fatalf("NewLayerBuilder: %v", err)
	}
	armLayer, err := b(arm64())
	if err != nil {
		t.Fatalf("arm64 build: %v", err)
	}
	files := layerFiles(t, armLayer)
	if got := files["/bin/x"]; got != "FALLBACK" {
		t.Errorf("arm64 should use fallback path, got %q (files: %v)", got, files)
	}
}

func TestNewLayerBuilder_ErrorWhenNoPathResolvesForPlatform(t *testing.T) {
	cfg := schema.Layer{LocalFile: schema.LocalFile{
		PathPerPlatform: map[string]string{"linux/amd64": "unused"},
	}}
	b, err := NewLayerBuilder(cfg)
	if err != nil {
		t.Fatalf("NewLayerBuilder: %v", err)
	}
	if _, err := b(arm64()); err == nil {
		t.Fatal("expected error for arm64 with no matching path")
	} else if !strings.Contains(err.Error(), "linux/arm64") {
		t.Errorf("error should name platform, got %v", err)
	}
}

func TestNewLayerBuilder_LocalDirIgnoresPlatform(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "A")

	cfg := schema.Layer{LocalDir: schema.LocalDir{Path: dir, ContainerPath: "/app"}}
	b, err := NewLayerBuilder(cfg)
	if err != nil {
		t.Fatalf("NewLayerBuilder: %v", err)
	}
	amdLayer, err := b(amd64())
	if err != nil {
		t.Fatalf("amd64 build: %v", err)
	}
	armLayer, err := b(arm64())
	if err != nil {
		t.Fatalf("arm64 build: %v", err)
	}
	amdDigest, _ := amdLayer.Digest()
	armDigest, _ := armLayer.Digest()
	if amdDigest != armDigest {
		t.Errorf("localDir should produce identical layer across platforms, got %s vs %s", amdDigest, armDigest)
	}
}

func TestNewLayerBuilder_RejectsBothLocalFileAndLocalDir(t *testing.T) {
	cfg := schema.Layer{
		LocalFile: schema.LocalFile{Path: "a"},
		LocalDir:  schema.LocalDir{Path: "b"},
	}
	_, err := NewLayerBuilder(cfg)
	if err == nil || !strings.Contains(err.Error(), "exactly one type") {
		t.Errorf("expected 'exactly one type' error, got %v", err)
	}
}

func TestNewLayerBuilder_RejectsEmptyConfig(t *testing.T) {
	_, err := NewLayerBuilder(schema.Layer{})
	if err == nil || !strings.Contains(err.Error(), "no layer builder config found") {
		t.Errorf("expected 'no layer builder config found' error, got %v", err)
	}
}

func TestNewLayerBuilder_LocalFilePropagatesConfigureError(t *testing.T) {
	// Invalid MaxSize surfaces an error from configure() and should bubble
	// up through the per-platform closure rather than panic.
	cfg := schema.Layer{LocalFile: schema.LocalFile{
		Path:    "/dev/null",
		MaxSize: "not-a-size",
	}}
	b, err := NewLayerBuilder(cfg)
	if err != nil {
		t.Fatalf("NewLayerBuilder: %v", err)
	}
	if _, err := b(amd64()); err == nil {
		t.Fatal("expected error from invalid MaxSize")
	}
}

func TestBuild_InvokesAllBuildersWithPlatform(t *testing.T) {
	var gotPlatforms []v1.Platform
	stub := func(p v1.Platform) (v1.Layer, error) {
		gotPlatforms = append(gotPlatforms, p)
		return nil, nil
	}
	_, err := Build([]LayerBuilder{stub, stub}, amd64())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(gotPlatforms) != 2 {
		t.Fatalf("expected two invocations, got %d", len(gotPlatforms))
	}
	for i, p := range gotPlatforms {
		if p.OS != "linux" || p.Architecture != "amd64" {
			t.Errorf("invocation %d: expected linux/amd64, got %s", i, p.String())
		}
	}
}

func TestBuild_WrapsErrorWithIndexAndPlatform(t *testing.T) {
	stub := func(p v1.Platform) (v1.Layer, error) {
		return nil, io.EOF
	}
	_, err := Build([]LayerBuilder{stub}, arm64())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "layer 0") || !strings.Contains(err.Error(), "linux/arm64") {
		t.Errorf("error should include index and platform, got %q", err.Error())
	}
}
