package schema

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	v1 "github.com/c9h-to/contain/pkg/schema/v1"
	"github.com/spf13/afero"
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
	// buf, err = removeYamlAnchors(buf)
	// if err != nil {
	// 	return nil, fmt.Errorf("unable to re-marshal YAML without dotted keys: %w", err)
	// }
	return parseConfig(buf)
}

func parseConfig(buf []byte) (v1.ContainConfig, error) {
	b := bytes.NewReader(buf)
	decoder := yaml.NewDecoder(b)
	decoder.KnownFields(true)
	var config v1.ContainConfig
	decoder.Decode(&config)
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
			stdin, err = ioutil.ReadAll(os.Stdin)
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

func ReadFile(filename string) ([]byte, error) {
	if !filepath.IsAbs(filename) {
		dir, err := os.Getwd()
		if err != nil {
			return []byte{}, err
		}
		filename = filepath.Join(dir, filename)
	}
	return afero.ReadFile(Fs, filename)
}
