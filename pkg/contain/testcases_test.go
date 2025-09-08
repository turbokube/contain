package contain_test

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
	"github.com/turbokube/contain/pkg/pushed"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	// SkipExpectIfDigestMatches can be used to speed up regression testing
	SkipExpectIfDigestMatches = false
)

// cases is an array because a testcase may depend on an output image from an earlier testcase
var cases = []testcases.Testcase{
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				Base: "contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4",
				Tag:  "contain-test/root:dot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          ".",
							ContainerPath: "/app",
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:449a8c029dae5a658300ca37f5f3ebaece877778f213a73803829fbcc520e91f",
		Expect: func(ref pushed.Artifact, t *testing.T) {

			// double check base image digest
			d, err := crane.Digest(fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4", testRegistry))
			Expect(err).NotTo(HaveOccurred())
			Expect(d).To(Equal("sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4"), "base digest")

			// head, err := remote.Head(ref.Reference(), testCraneOptions.Remote...)
			// Expect(err).To(BeNil())
			// Expect(head.Digest.String()).To(Equal("sd"))

			img, err := remote.Image(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())

			// this is probably the manifest of one of the layers, thus ^ is using Image wrong
			raw, err := img.RawManifest()
			Expect(err).To(BeNil())
			var manifest map[string]interface{}
			Expect(json.Unmarshal(raw, &manifest)).To(BeNil())
			Expect(manifest["schemaVersion"]).To(Equal(2.0))
			Expect(manifest["mediaType"]).To(Equal("application/vnd.oci.image.manifest.v1+json"))

			// Additional structured assertions replacing prior brittle MatchJSON
			var mStruct v1.Manifest
			Expect(json.Unmarshal(raw, &mStruct)).To(Succeed())
			// Config assertions
			Expect(string(mStruct.Config.MediaType)).To(Equal("application/vnd.oci.image.config.v1+json"))
			Expect(mStruct.Config.Size).To(BeNumerically(">", 0))
			cfgName, err := img.ConfigName()
			Expect(err).NotTo(HaveOccurred())
			Expect(mStruct.Config.Digest.String()).To(Equal(cfgName.String()))
			// Layer assertions (we expect exactly 2: base layer + appended layer)
			Expect(len(mStruct.Layers)).To(Equal(2))
			layer0 := mStruct.Layers[0]
			layer1 := mStruct.Layers[1]
			Expect(string(layer0.MediaType)).To(Equal("application/vnd.oci.image.layer.v1.tar+gzip"))
			Expect(layer0.Size).To(BeNumerically(">", 0))
			if layer0.Annotations != nil { // buildkit timestamp annotation on first layer
				Expect(layer0.Annotations["buildkit/rewritten-timestamp"]).To(Equal("0"))
			}
			Expect(string(layer1.MediaType)).To(Equal("application/vnd.docker.image.rootfs.diff.tar.gzip"))
			Expect(layer1.Size).To(BeNumerically(">", 0))
			Expect(layer1.Digest.String()).NotTo(Equal(layer0.Digest.String()))
			Expect(layer0.Digest.String()).NotTo(Equal(mStruct.Config.Digest.String()))
			Expect(layer1.Digest.String()).NotTo(Equal(mStruct.Config.Digest.String()))

			anns, _ := manifest["annotations"].(map[string]interface{})
			if anns == nil {
				t.Fatalf("expected annotations")
			}
			baseDigest, ok1 := anns["org.opencontainers.image.base.digest"].(string)
			baseName, ok2 := anns["org.opencontainers.image.base.name"].(string)
			Expect(ok1).To(BeTrue(), "base digest annotation present")
			Expect(ok2).To(BeTrue(), "base name annotation present")
			overrideHost := os.Getenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE")
			if overrideHost == "" {
				overrideHost = testRegistry
			}
			expectedName := fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest", overrideHost)
			Expect(baseName).To(Equal(expectedName))
			Expect(baseDigest).To(Equal("sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4"))

			m, err := remote.Get(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			Expect(m.MediaType.IsIndex()).To(BeTrue())
			idx, err := m.ImageIndex()
			Expect(err).To(BeNil())
			im, err := idx.IndexManifest()
			Expect(err).To(BeNil())
			Expect(len(im.Manifests)).To(Equal(2))
			for _, d := range im.Manifests {
				Expect(string(d.MediaType)).To(Equal("application/vnd.oci.image.manifest.v1+json"))
				Expect(d.Platform).NotTo(BeNil())
				Expect(d.Platform.Architecture).NotTo(BeEmpty())
				Expect(d.Digest.Hex).NotTo(BeEmpty())
			}

			amd64 := v1.Platform{Architecture: "amd64", OS: "linux"}
			amd64options := append(testCraneOptions.Remote, remote.WithPlatform(amd64))
			amd64img, err := remote.Image(ref.Reference(), amd64options...)
			if err != nil {
				t.Error(err)
			}
			amd64layers, err := amd64img.Layers()
			if err != nil {
				t.Error(err)
			}
			amd64config, err := amd64img.RawConfigFile()
			if err != nil {
				t.Error(err)
			}
			amd64cfg, err := amd64img.ConfigFile()
			if err != nil {
				t.Error(err)
			}
			if amd64cfg.Config.WorkingDir != "/" {
				t.Errorf("workingdir %s", amd64cfg.Config.WorkingDir)
			}
			Expect(amd64config).To(MatchJSON(`{"architecture":"amd64","created":"1970-01-01T00:00:00Z","history":[{"created":"1970-01-01T00:00:00Z","created_by":"ARG TARGETARCH","comment":"buildkit.dockerfile.v0","empty_layer":true},{"created":"1970-01-01T00:00:00Z","created_by":"COPY ./amd64 / # buildkit","comment":"buildkit.dockerfile.v0"},{"created":"0001-01-01T00:00:00Z"}],"os":"linux","rootfs":{"type":"layers","diff_ids":["sha256:294329baf7cfd56cfce463c90292879d44d563febc3f77a4c4f4ba8bf0e07a24","sha256:a800f38d8d6b28428d0e23bc257e614ead7d9af4c80470d8771b5926b78c30a8"]},"config":{"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],"WorkingDir":"/"}}`))

			zap.L().Debug("amd64", zap.Int("layers", len(amd64layers)))

			var fs = make(map[string]*tar.Header)

			tr := tar.NewReader(mutate.Extract(amd64img))
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break // End of archive
				}
				if err != nil {
					t.Error(err)
					t.FailNow()
				}
				fs[hdr.Name] = hdr
				zap.L().Debug(hdr.Name,
					zap.Any("AccessTime", hdr.AccessTime),
					zap.Any("ChangeTime", hdr.ChangeTime),
					zap.Any("Format", hdr.Format),
					zap.Any("Uid", hdr.Uid),
					zap.Any("Gid", hdr.Gid),
					zap.Any("Uname", hdr.Uname),
					zap.Any("Gname", hdr.Gname),
					zap.Any("Linkname", hdr.Linkname),
					zap.Any("ModTime", hdr.ModTime),
					zap.Any("Mode", hdr.Mode),
					zap.Any("PAXRecords", hdr.PAXRecords),
					zap.Any("Size", hdr.Size),
					zap.Any("Typeflag", hdr.Typeflag),
					zap.Any("FileInfo.Mode", hdr.FileInfo().Mode()),
					zap.Any("FileInfo.Sys", hdr.FileInfo().Sys()),
				)
			}

			b := fs["amd64"]
			Expect(b).NotTo(BeNil(), "fs should contain file from the base index's platform image")
			Expect(b.Mode == 420).To(BeTrue(), "should be -rw-r--r--")
			Expect(b.ModTime).To(Equal(time.Unix(0, 0)))

			a := fs["/app/root.txt"]
			Expect(a).NotTo(BeNil(), "fs should contain file from the appended layer")
			Expect(a.Mode == 493).To(BeTrue(), "should be -rwxr-xr-x (executable bit preserved from source)")
			Expect(a.ModTime).To(Equal(time.Unix(0, 0)))

			arm64 := v1.Platform{Architecture: "arm64", OS: "linux"}
			arm64options := append(testCraneOptions.Remote, remote.WithPlatform(arm64))
			arm64img, err := remote.Image(ref.Reference(), arm64options...)
			if err != nil {
				t.Error(err)
			}
			arm64layers, err := arm64img.Layers()
			if err != nil {
				t.Error(err)
			}
			arm64config, err := arm64img.RawConfigFile()
			if err != nil {
				t.Error(err)
			}
			arm64cfg, err := arm64img.ConfigFile()
			if err != nil {
				t.Error(err)
			}
			if arm64cfg.Config.WorkingDir != "/" {
				t.Errorf("workingdir %s", arm64cfg.Config.WorkingDir)
			}
			Expect(arm64config).To(MatchJSON(`{"architecture":"arm64","created":"1970-01-01T00:00:00Z","history":[{"created":"1970-01-01T00:00:00Z","created_by":"ARG TARGETARCH","comment":"buildkit.dockerfile.v0","empty_layer":true},{"created":"1970-01-01T00:00:00Z","created_by":"COPY ./arm64 / # buildkit","comment":"buildkit.dockerfile.v0"},{"created":"0001-01-01T00:00:00Z"}],"os":"linux","rootfs":{"type":"layers","diff_ids":["sha256:716e2984b8fca92562cff105a2fe22f4f2abdfa6ae853b72024ea2f2d1741a39","sha256:a800f38d8d6b28428d0e23bc257e614ead7d9af4c80470d8771b5926b78c30a8"]},"config":{"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],"WorkingDir":"/"}}`))

			zap.L().Debug("arm64", zap.Int("layers", len(arm64layers)))
			// we should assert on fs contents but we need an abstraction for the tar assertions above

			Expect(string(ref.MediaType)).To(Equal("application/vnd.oci.image.index.v1+json"))
			// Convert platforms to string slice if needed
			var pf []string
			for _, p := range ref.Platforms {
				pf = append(pf, p.String())
			}
			Expect(pf).To(Equal([]string{"linux/amd64", "linux/arm64"}))
		},
	},
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				Base: "contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4",
				Tag:  "contain-test/root:dot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          ".",
							ContainerPath: "/1",
						},
					},
				},
				Platforms: []string{"linux/amd64"},
			}
		},
		ExpectDigest: "sha256:58abaf10c628fad1c9f9e4802c9a11bed0ad0452361e3c77d115aff0dae7038c",
		Expect: func(ref pushed.Artifact, t *testing.T) {
			img, err := remote.Get(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			Expect(img.MediaType.IsIndex()).NotTo(BeTrue())
			Expect(img.MediaType.IsImage()).To(BeTrue())
			Expect(string(img.MediaType)).To(Equal("application/vnd.oci.image.manifest.v1+json"))
			Expect(string(ref.MediaType)).To(Equal("application/vnd.oci.image.manifest.v1+json"))
			var pf []string
			for _, p := range ref.Platforms {
				pf = append(pf, p.String())
			}
			Expect(pf).To(Equal([]string{"linux/amd64"}))
		},
	},
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				// Base here has attestation layers, they should not be appended to
				Base: "contain-test/baseimage-multiarch1:latest@sha256:c5653a3316b7217a0e7e2adec8ba8d344ba0815367aad8bd5513c9f6ca85834d",
				Tag:  "contain-test/root:dot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          ".",
							ContainerPath: "/1",
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:7399a2da270aae9c9dcf7fc008c3c161f4425faf78c187b4c7fbab0e02ee7dde",
		Expect: func(ref pushed.Artifact, t *testing.T) {
			img, err := remote.Get(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			Expect(img.MediaType.IsIndex()).To(BeTrue())
			index, err := img.ImageIndex()
			Expect(err).To(BeNil())
			indexManifest, err := index.IndexManifest()
			Expect(err).To(BeNil())
			Expect(len(indexManifest.Manifests)).To(Equal(2), "attestation manifests are currently not supported and should thus be dropped")
			Expect(string(ref.MediaType)).To(Equal("application/vnd.oci.image.index.v1+json"))
			var pf []string
			for _, p := range ref.Platforms {
				pf = append(pf, p.String())
			}
			Expect(pf).To(Equal([]string{"linux/amd64", "linux/arm64"}))
		},
	},
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("f.txt", "")
			return schema.ContainConfig{
				// Base here has attestation layers, they should not be appended to
				Base: "contain-test/baseimage-multiarch1:latest@sha256:c5653a3316b7217a0e7e2adec8ba8d344ba0815367aad8bd5513c9f6ca85834d",
				Tag:  "contain-test/root:dot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          ".",
							ContainerPath: "/dir",
						},
						Attributes: schema.LayerAttributes{
							Uid:      1234,
							Gid:      5678,
							FileMode: 0750,
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:724714f1082b6836d5b1db3923b3e2675c3dddf5e355c5f04b5da6ab1fdff397",
		Expect: func(ref pushed.Artifact, t *testing.T) {
			img, err := remote.Image(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			var fs = make(map[string]*tar.Header)
			tr := tar.NewReader(mutate.Extract(img))
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break // End of archive
				}
				if err != nil {
					t.Error(err)
					t.FailNow()
				}
				fs[hdr.Name] = hdr
			}
			b := fs["/dir/f.txt"]
			Expect(b).NotTo(BeNil(), "fs should contain the appended file")
			Expect(b.FileInfo().Mode().String()).To(Equal("-rwxr-x---"))
			Expect(b.Uid).To(Equal(1234))
			Expect(b.Gid).To(Equal(5678))
		},
	},
	{
		// this LocalFile should layer should be identical to the LocalDir above and thus so shuld the Expect
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("f.txt", "")
			return schema.ContainConfig{
				// Base here has attestation layers, they should not be appended to
				Base: "contain-test/baseimage-multiarch1:latest@sha256:c5653a3316b7217a0e7e2adec8ba8d344ba0815367aad8bd5513c9f6ca85834d",
				Tag:  "contain-test/root:file",
				Layers: []schema.Layer{
					{
						LocalFile: schema.LocalFile{
							Path:          "f.txt",
							ContainerPath: "/dir/f.txt",
						},
						Attributes: schema.LayerAttributes{
							Uid:      1234,
							Gid:      5678,
							FileMode: 0750,
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:229888fcbf659f453173f495ee645b19f0659f90359959e85886b45fdb5396e3",
		Expect: func(ref pushed.Artifact, t *testing.T) {
			img, err := remote.Image(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			var fs = make(map[string]*tar.Header)
			tr := tar.NewReader(mutate.Extract(img))
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break // End of archive
				}
				if err != nil {
					t.Error(err)
					t.FailNow()
				}
				fs[hdr.Name] = hdr
			}
			b := fs["/dir/f.txt"]
			Expect(b).NotTo(BeNil(), "fs should contain the appended file")
			Expect(b.FileInfo().Mode().String()).To(Equal("-rwxr-x---"))
			Expect(b.Uid).To(Equal(1234))
			Expect(b.Gid).To(Equal(5678))
		},
	},
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("x.txt", "x")
			return schema.ContainConfig{
				Base: "contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4",
				Tag:  "contain-test/envs:test",
				Layers: []schema.Layer{
					{LocalDir: schema.LocalDir{Path: ".", ContainerPath: "/env"}},
				},
				Platforms: []string{"linux/amd64"},
				Envs: []schema.Env{
					{Name: "FOO", Value: "bar"},
					{Name: "PATH", Value: "/custom/bin"}, // override existing PATH
				},
			}
		},
		ExpectDigest: "sha256:d164b233af09e91f3461a15e9c3ac7ab1465055b20374a51f21cb9b739111251",
		Expect: func(ref pushed.Artifact, t *testing.T) {
			img, err := remote.Image(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			cfg, err := img.ConfigFile()
			Expect(err).To(BeNil())
			var foundFoo, foundPath bool
			for _, e := range cfg.Config.Env {
				if e == "FOO=bar" {
					foundFoo = true
				}
				if e == "PATH=/custom/bin" {
					foundPath = true
				}
			}
			Expect(foundFoo).To(BeTrue(), "FOO env present")
			Expect(foundPath).To(BeTrue(), "PATH override applied")
		},
	},
}

func TestTestcases(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	t.Run(fmt.Sprintf("#- %s", testRegistry), func(t *testing.T) {
		resp, err := http.Head(fmt.Sprintf("http://%s/v2/", testRegistry))
		if err != nil {
			t.Error(err)
		}
		if resp.Status != "200 OK" {
			t.Errorf("%s %s", testRegistry, resp.Status)
		}
		fmt.Printf("#- %s OK\n", testRegistry)
	})

	fmt.Printf("# cases: %d\n", len(cases))
	for i, testcase := range cases {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			RegisterTestingT(t)
			// logs an initial zap entry because the ordering of test output might be confusing
			zap.L().Debug("DEBUG", zap.Int("case", i))
			if len(testcase.ExpectDigest) != 71 {
				t.Errorf("digest %s", testcase.ExpectDigest)
			}
			dir := testcases.NewTempDir(t)
			c := testcase.RunConfig(nil, dir)

			// this output is helpful in combination with dagger output
			t.Logf("\n#%d %s -> %s\n", i, c.Base, c.Tag)

			c.Base = fmt.Sprintf("%s/%s", testRegistry, c.Base)
			c.Tag = fmt.Sprintf("%s/%s", testRegistry, c.Tag)

			chdir := appender.NewChdir(dir.Root())
			defer chdir.Cleanup()

			// result, err := contain.Run(c)
			// if err != nil {
			// 	t.Errorf("libcontain run %v", err)
			// }
			// Use separate invocations to simplify debugging

			layers, err := contain.RunLayers(c)
			Expect(err).NotTo(HaveOccurred())
			zap.L().Debug("testcase layers", zap.Int("count", len(layers)))
			buildOutput, err := contain.RunAppend(c, layers)
			Expect(err).NotTo(HaveOccurred())
			if buildOutput == nil {
				t.Fatalf("nil buildOutput")
			}
			if buildOutput.Skaffold == nil || len(buildOutput.Skaffold.Builds) == 0 {
				t.Fatalf("Zero builds in buildOutput: %v", buildOutput)
			}
			result := buildOutput.Artifact()

			// Log actual result digest for updating ExpectDigest values
			actual := result.Http().Hash.String()
			fmt.Printf("case%d built digest: %s (override host localhost:12345)\n", i, actual)
			if actual != testcase.ExpectDigest {
				// Fail fast with helpful message
				t.Fatalf("digest mismatch: got %s expected %s", actual, testcase.ExpectDigest)
			}
			// Always run expectations; digest equality asserted via ExpectDigest once updated.
			if testcase.Expect != nil {
				testcase.Expect(result, t)
			}
		})
		// fmt.Printf("## CASE: %d\n", i)
		// r := runner(testcase)
		// t.Run(fmt.Sprintf("testcase %d", i), r)
		// t.Errorf("err %d\n", i)
	}
}
