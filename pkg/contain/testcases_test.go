package contain_test

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
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
		ExpectDigest: "sha256:5fbdcb2ac528ffe3aa2c5a7678d098b526eb7d3916d0bbee836549b0e20d746a",
		Expect: func(ref contain.Artifact, t *testing.T) {

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
			// layers := manifest["layers"].([]interface{})
			Expect(raw).To(MatchJSON(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","size":639,"digest":"sha256:e451487e9f07fa55dc57c2a59f0e964ee7e5b8a05d3cc81cb1392812d07929c3"},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","size":80,"digest":"sha256:ac770dd5cf15356232a70ab6d2689e60b39b23fffe1c10955ba2681d32a4ad15","annotations":{"buildkit/rewritten-timestamp":"0"}},{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":107,"digest":"sha256:6c9f141295d5636893db1435b5a20917860516e5e772445fb08bc240af66e57b"}]}`))

			m, err := remote.Get(ref.Reference(), testCraneOptions.Remote...)
			Expect(err).To(BeNil())
			Expect(m.Digest.Hex).To(Equal("5fbdcb2ac528ffe3aa2c5a7678d098b526eb7d3916d0bbee836549b0e20d746a"))
			Expect(m.RawManifest()).To(MatchJSON(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","size":611,"digest":"sha256:1c94651a07505a79ab6c6a330b1bb643e1ba5141216f276cddfc0bcef4a05f05","platform":{"architecture":"amd64","os":"linux"}},{"mediaType":"application/vnd.oci.image.manifest.v1+json","size":611,"digest":"sha256:d38cbbfbb5b8c33cc0f9eb47c559e7a12939644a28fd60c7e5119c0f62546dbe","platform":{"architecture":"arm64","os":"linux"}}]}`))

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
			Expect(amd64config).To(MatchJSON(`{"architecture":"amd64","created":"1970-01-01T00:00:00Z","history":[{"created":"1970-01-01T00:00:00Z","created_by":"ARG TARGETARCH","comment":"buildkit.dockerfile.v0","empty_layer":true},{"created":"1970-01-01T00:00:00Z","created_by":"COPY ./amd64 / # buildkit","comment":"buildkit.dockerfile.v0"},{"created":"0001-01-01T00:00:00Z"}],"os":"linux","rootfs":{"type":"layers","diff_ids":["sha256:294329baf7cfd56cfce463c90292879d44d563febc3f77a4c4f4ba8bf0e07a24","sha256:fc2b7873b55585e496924fc15d8fbb4286e708b3d2434bbe8fa1d1711953c151"]},"config":{"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],"WorkingDir":"/"}}`))

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
			Expect(a.Mode == 420).To(BeTrue(), "should be -rw-r--r--")
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
			Expect(arm64config).To(MatchJSON(`{"architecture":"arm64","created":"1970-01-01T00:00:00Z","history":[{"created":"1970-01-01T00:00:00Z","created_by":"ARG TARGETARCH","comment":"buildkit.dockerfile.v0","empty_layer":true},{"created":"1970-01-01T00:00:00Z","created_by":"COPY ./arm64 / # buildkit","comment":"buildkit.dockerfile.v0"},{"created":"0001-01-01T00:00:00Z"}],"os":"linux","rootfs":{"type":"layers","diff_ids":["sha256:716e2984b8fca92562cff105a2fe22f4f2abdfa6ae853b72024ea2f2d1741a39","sha256:fc2b7873b55585e496924fc15d8fbb4286e708b3d2434bbe8fa1d1711953c151"]},"config":{"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],"WorkingDir":"/"}}`))

			zap.L().Debug("arm64", zap.Int("layers", len(arm64layers)))
			// we should assert on fs contents but we need an abstraction for the tar assertions above
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
		ExpectDigest: "sha256:--todo--",
		Expect: func(ref contain.Artifact, t *testing.T) {

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
		ExpectDigest: "sha256:b1f5d00014e713ed568b951280056828eb5ab6a3a90c9a73b0ea8e1d0749dc90",
		Expect: func(ref contain.Artifact, t *testing.T) {

		},
	},
}

// func runner(testcase testcases.Testcase) func(t *testing.T) {
// 	return func(t *testing.T) {

// 	}
// }

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
			if len(buildOutput.Builds) == 0 {
				t.Fatalf("Zero builds in buildOutput: %v", buildOutput)
			}
			result := buildOutput.Builds[0]

			expectRef := fmt.Sprintf("%s@%s", c.Tag, testcase.ExpectDigest)
			if result.Tag != expectRef || !SkipExpectIfDigestMatches {
				if testcase.Expect == nil {
					t.Error("missing Expect func")
				} else {
					testcase.Expect(result, t)
				}
				if result.Tag != expectRef {
					t.Errorf("pushed   %s\n                   expected %s", result.Tag, expectRef)
				}
			}
		})
		// fmt.Printf("## CASE: %d\n", i)
		// r := runner(testcase)
		// t.Run(fmt.Sprintf("testcase %d", i), r)
		// t.Errorf("err %d\n", i)
	}
}
