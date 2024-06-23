package schema

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/GoogleContainerTools/skaffold/v2/pkg/skaffold/tags"
	"github.com/spf13/afero"
	v1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	yaml "gopkg.in/yaml.v3"
)

// Fs is the underlying filesystem to use for reading skaffold project files & configuration.  OS FS by default
var Fs = afero.NewOsFs()

var stdin []byte

// ParseConfig reads a configuration file.
func ParseConfig(filename string) (v1.ContainConfig, error) {
	noconfig := v1.ContainConfig{}
	buf, err := ReadConfiguration(filename)
	if err != nil {
		return noconfig, fmt.Errorf("read contain config: %w", err)
	}
	return Parse(buf)
}

func Parse(buf []byte) (v1.ContainConfig, error) {
	noconfig := v1.ContainConfig{}
	// https://github.com/GoogleContainerTools/skaffold/blob/v2.12.0/pkg/skaffold/schema/versions.go#L231
	// buf, err = removeYamlAnchors(buf)
	// if err != nil {
	// 	return nil, fmt.Errorf("unable to re-marshal YAML without dotted keys: %w", err)
	// }
	config, err := parseConfig(buf)
	if err != nil {
		return noconfig, err
	}
	if err = tags.ApplyTemplates(&config); err != nil {
		return noconfig, fmt.Errorf("apply templates: %w\n%s", err, string(buf))
	}
	// tags.MakeFilePathsAbsolute(config)
	return config, nil
}

func parseConfig(buf []byte) (v1.ContainConfig, error) {
	b := bytes.NewReader(buf)
	decoder := yaml.NewDecoder(b)
	decoder.KnownFields(true)
	var config v1.ContainConfig
	err := decoder.Decode(&config)
	if err == io.EOF {
		// skaffold handles multiple configs: https://github.com/GoogleContainerTools/skaffold/blob/v2.12.0/pkg/skaffold/schema/versions.go#L320
		return config, fmt.Errorf("config EOF: %w", err)
	}
	if err != nil {
		return config, fmt.Errorf("unable to parse config: %w", err)
	}
	config.Status.Sha256 = fmt.Sprintf("%x", sha256.Sum256(buf))
	config.Status.Md5 = fmt.Sprintf("%x", md5.Sum(buf))
	return config, nil
}

// ReadConfiguration reads config and returns content
func ReadConfiguration(filePath string) ([]byte, error) {
	// https://github.com/GoogleContainerTools/skaffold/blob/v2.2.0/pkg/skaffold/util/config.go#L38
	switch {
	case filePath == "":
		return nil, errors.New("filename not specified")
	case filePath == "-":
		if len(stdin) == 0 {
			var err error
			stdin, err = io.ReadAll(os.Stdin)
			if err != nil {
				return []byte{}, err
			}
		}
		return stdin, nil
	// case IsURL(filePath):
	// 	return Download(filePath)
	default:
		if !filepath.IsAbs(filePath) {
			dir, err := os.Getwd()
			if err != nil {
				zap.L().Error("get absolute path for config",
					zap.String("path", filePath),
					zap.Error(err),
				)
				return []byte{}, err
			}
			filePath = filepath.Join(dir, filePath)
		}
		contents, err := afero.ReadFile(Fs, filePath)
		if err != nil {
			return []byte{}, err
		}

		return contents, err
	}
}
