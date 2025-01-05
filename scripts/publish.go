package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-github/v50/github"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	owner = "turbokube"
	repo  = "contain"
)

var (
	publishVersion    = "0.5.5"
	releaseBinaryName = regexp.MustCompile(`^contain-v(?P<version>\d+\.\d+\.\d+)-(?P<os>[a-z0-9]+)-(?P<arch>[a-z0-9]+)(?P<ext>\.exe)?(?P<checksum>\.[a-z0-9]+)?$`)
)

type OS int

type CPU int

type ContainBin struct {
	Contain string `json:"contain"`
}

type ParentPackage struct {
	Name     string `json:"name"`
	Homepage string `json:"homepage"`
	Licence  string `json:"license"`
}

type BinPackage struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Homepage    string            `json:"homepage,omitempty"`
	Repository  string            `json:"repository,omitempty"`
	Licence     string            `json:"license,omitempty"`
	Bin         map[string]string `json:"bin"`
	Os          []OS              `json:"os"`
	Cpu         []CPU             `json:"cpu"`
}

const (
	Darwin OS  = 1
	Linux  OS  = 2
	Win32  OS  = 3
	X64    CPU = 1
	Arm64  CPU = 2
)

func (os OS) String() string {
	switch os {
	case Darwin:
		return "darwin"
	case Linux:
		return "linux"
	case Win32:
		return "win32"
	default:
		panic(fmt.Sprintf("os name %d", os))
	}
}

func (c CPU) String() string {
	switch c {
	case X64:
		return "x64"
	case Arm64:
		return "arm64"
	default:
		panic(fmt.Sprintf("cpu name %d", c))
	}
}

func NewCPU(arch string) CPU {
	switch arch {
	case "amd64":
		return X64
	case "arm64":
		return Arm64
	default:
		panic(fmt.Sprintf("arch: %s", arch))
	}
}

func NewOs(os string) OS {
	switch os {
	case "darwin":
		return Darwin
	case "linux":
		return Linux
	case "windows":
		return Win32
	default:
		panic(fmt.Sprintf("os: %s", os))
	}
}

func (os OS) MarshalJSON() ([]byte, error) {
	return json.Marshal(os.String())
}

func (cpu CPU) MarshalJSON() ([]byte, error) {
	return json.Marshal(cpu.String())
}

func main() {
	ctx := context.TODO()

	consoleDebugging := zapcore.Lock(os.Stderr)
	consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	consoleEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return true
	})
	core := zapcore.NewCore(consoleEncoder, consoleDebugging, consoleEnabler)
	logger := zap.New(core)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	var publishTag *github.RepositoryTag
	var publishRelease *github.RepositoryRelease

	var err error

	parent := ParentPackage{}
	parentP, err := ioutil.ReadFile("package.json")
	if err != nil {
		zap.L().Fatal("read package.json", zap.Error(err))
	}
	if err := json.Unmarshal(parentP, &parent); err != nil {
		zap.L().Fatal("unmarshal package.json", zap.Error(err))
	}

	client := github.NewClient(nil)
	repository, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		zap.L().Fatal("repository access", zap.Error(err))
	}

	tags, _, err := client.Repositories.ListTags(ctx, owner, repo, nil)
	if err != nil {
		zap.L().Fatal("tags access", zap.Error(err))
	}
	for _, tag := range tags {
		if *tag.Name == fmt.Sprintf("v%s", publishVersion) {
			publishTag = tag
		}
		zap.L().Debug("tag", zap.String("name", *tag.Name), zap.String("sha", *tag.Commit.SHA))
	}

	releases, _, err := client.Repositories.ListReleases(ctx, owner, repo, nil)
	if err != nil {
		zap.L().Fatal("releases access", zap.Error(err))
	}
	for _, release := range releases {
		if *release.TagName == *publishTag.Name {
			publishRelease = release
		}
		zap.L().Debug("release", zap.String("tag", *release.TagName))
	}

	if publishRelease == nil {
		zap.L().Warn("not released yet", zap.String("tag", *publishTag.Name))
		publishRelease, err = releaseFromTag(ctx, client, repository, publishTag)
		if err != nil {
			zap.L().Fatal("release from tag", zap.Error(err))
		}
	}

	var remainingWork = make([]string, 0)
	npm, err := filepath.Abs("npm")
	if err != nil {
		zap.L().Fatal("parent dir", zap.Error(err))
	}
	for _, asset := range publishRelease.Assets {
		match := releaseBinaryName.FindStringSubmatch(*asset.Name)
		zap.L().Debug("asset", zap.String("name", *asset.Name), zap.Strings("match", match))
		if match[5] != "" {
			zap.L().Debug("ignore", zap.String("name", *asset.Name))
			continue
		}
		version := match[1]
		o := NewOs(match[2])
		cpu := NewCPU(match[3])
		exename := "contain"
		binname := fmt.Sprintf("%s-%s-%s", exename, o.String(), cpu.String())
		if o.String() == "win32" {
			exename = fmt.Sprintf("%s.exe", exename)
			binname = fmt.Sprintf("%s.exe", binname)
		}

		p := BinPackage{
			Name:        fmt.Sprintf("contain-%s-%s", o, cpu),
			Version:     version,
			Homepage:    parent.Homepage,
			Description: fmt.Sprintf("Platform specific (%s-%s) binary package for %s", o, cpu, parent.Name),
			Bin: map[string]string{
				binname: fmt.Sprintf("bin/%s", exename),
			},
			Licence: parent.Licence,
			Os:      []OS{o},
			Cpu:     []CPU{cpu},
		}
		dir := path.Join(npm, p.Name)
		bindir := path.Join(dir, "bin")
		if err := os.MkdirAll(bindir, 0755); err != nil {
			zap.L().Fatal("package dir", zap.Error(err))
		}
		oldbins, err := os.ReadDir(bindir)
		if err != nil {
			zap.L().Fatal("list existing", zap.String("dir", bindir), zap.Error(err))
		}
		for _, old := range oldbins {
			if err := os.Remove(path.Join(bindir, old.Name())); err != nil {
				zap.L().Fatal("remove existing", zap.String("name", old.Name()), zap.Error(err))
			}
		}
		j, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			zap.L().Fatal("marshal package.json", zap.Error(err))
		}
		if err := ioutil.WriteFile(path.Join(dir, "package.json"), j, 0644); err != nil {
			zap.L().Fatal("write package.json", zap.Error(err))
		}
		bin := path.Join(dir, p.Bin[binname])
		out, err := os.Create(bin)
		if err != nil {
			zap.L().Fatal("create download target", zap.String("path", bin), zap.Error(err))
		}
		defer out.Close()
		url := asset.GetBrowserDownloadURL()
		download, err := http.Get(url)
		if err != nil {
			zap.L().Fatal("download", zap.String("url", url), zap.Error(err))
		}
		defer download.Body.Close()
		n, err := io.Copy(out, download.Body)
		if err != nil {
			zap.L().Fatal("download body", zap.String("url", url), zap.String("to", bin), zap.Error(err))
		}
		if err := out.Chmod(0755); err != nil {
			zap.L().Fatal("bin chmod", zap.Error(err))
		}
		zap.L().Info("generated package", zap.String("at", dir), zap.Int64("binSize", n))
		remainingWork = append(remainingWork, fmt.Sprintf("(cd npm/%s; npm publish --access public)", p.Name))
	}
	remainingWork = append(remainingWork, "npm publish --access public")
	fmt.Println(strings.Join(remainingWork, "\n"))
}

func releaseFromTag(ctx context.Context, client *github.Client, repository *github.Repository, tag *github.RepositoryTag) (*github.RepositoryRelease, error) {
	// or "Create release" from the ...-button at https://github.com/turbokube/contain/tags
	zap.L().Fatal("TODO publish manually", zap.String("at", *repository.TagsURL))
	return nil, nil
}
