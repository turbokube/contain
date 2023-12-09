package contain_test

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

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

// cases is an array because a testcase may depend on an output image from an earlier testcase
var cases = []testcases.Testcase{
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				Base: "contain-test/multiarch-base:noattest",
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
		ExpectDigest: "sha256:---TODO-repeatable-test-builds----------------------------------",
		Expect: func(ref contain.Artifact, t *testing.T) {

			// double check base image digest
			d, err := crane.Digest(fmt.Sprintf("%s/contain-test/multiarch-base:noattest", testRegistry))
			if err != nil {
				t.Error(err)
			}
			if d != "sha256:ad170cac387bea5246c9b85f60077b02ebf814d8b151568ad0d35c9b09097b74" {
				t.Errorf("base digest %s", d)
			}

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
			Expect(manifest["mediaType"]).To(Equal("application/vnd.docker.distribution.manifest.v2+json"))
			// layers := manifest["layers"].([]interface{})
			Expect(raw).To(MatchJSON(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"mediaType":"application/vnd.docker.container.image.v1+json","size":669,"digest":"sha256:0aaa7da713d96061473b1d2a702b0fcae2d65d10479e5472d44bd2fc507f3aee"},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":93,"digest":"sha256:c61587a79a418fb6188de8add2e9f694b012acde27abefd27dedaff5f02de71e"},{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":107,"digest":"sha256:6c9f141295d5636893db1435b5a20917860516e5e772445fb08bc240af66e57b"}],"annotations":{"org.opencontainers.image.base.digest":"sha256:15ea7700e453827d5c394519a17e8f3b6086a42a9c843b134d703bc082f499c4","org.opencontainers.image.base.name":"/contain-test/multiarch-base:noattest"}}`))

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

			os.Stdout.Write(amd64config)
			fmt.Println()

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
				fmt.Printf("---- contents of %s: ----\n", hdr.Name)
				if _, err := io.Copy(os.Stdout, tr); err != nil {
					t.Error(err)
					t.FailNow()
				}
				fmt.Printf("----     end of %s   ----\n", hdr.Name)
			}

			if fs["/app/root.txt"].Mode != 0420 {
				t.Errorf("mode %v", fs["/app/root.txt"].Mode)
			}

			if fs["/app/root.txt"].Mode != 0420 {
				t.Errorf("mode %v", fs["/app/root.txt"].Mode)
			}

			// https://github.com/google/go-containerregistry/blob/55ffb0092afd1313edad861a553b4fcea21b4da2/pkg/crane/export.go#L27
			// or Extract
			// filesystem or layer to tar
			// abstraction on top of tar?
		},
	},
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				Base: "contain-test/multiarch-base:noattest",
				Tag:  "contain-test/root:nodot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          "",
							ContainerPath: "/app",
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:---TODO-repeatable-test-builds----------------------------------",
		Expect: func(ref contain.Artifact, t *testing.T) {
			fmt.Printf("TODO expect %s\n", ref.Tag)
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
			fmt.Printf("\n#%d %s -> %s\n", i, c.Base, c.Tag)

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
			if err != nil {
				t.Errorf("layers %v", err)
			}
			zap.L().Debug("testcase layers", zap.Int("count", len(layers)))
			buildOutput, err := contain.RunAppend(c, layers)
			if err != nil {
				t.Errorf("append %v", err)
			}
			result := buildOutput.Builds[0]

			expectRef := fmt.Sprintf("%s@%s", c.Tag, testcase.ExpectDigest)
			if result.Tag != expectRef {
				if testcase.Expect == nil {
					t.Error("missing Expect func")
				} else {
					testcase.Expect(result, t)
				}
				t.Errorf("pushed   %s\n                   expected %s", result.Tag, expectRef)
			}
		})
		// fmt.Printf("## CASE: %d\n", i)
		// r := runner(testcase)
		// t.Run(fmt.Sprintf("testcase %d", i), r)
		// t.Errorf("err %d\n", i)
	}
}
