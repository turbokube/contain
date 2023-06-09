package run

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
)

func LayerToContainer(layer v1.Layer, target *SyncTarget) error {
	// https://github.com/GoogleContainerTools/skaffold/blob/v2.2.0/pkg/skaffold/sync/kubectl.go#L49
	arg := []string{
		"exec", target.Pod.Name,
		"--namespace", target.Pod.Namespace,
		"-c", target.Container.Name,
		"-i",
		"--",
		"tar",
		"xvzmf",
		"-",
		"-C",
		"/",
		"--no-same-owner",
	}
	addEnv := []string{}
	copyCmd := exec.Command("kubectl", arg...)
	copyCmd.Env = append(os.Environ(), addEnv...)
	var outbuf, errbuf bytes.Buffer
	copyCmd.Stdout = &outbuf
	copyCmd.Stderr = &errbuf

	var err error
	copyCmd.Stdin, err = layer.Compressed()
	if err != nil {
		return err
	}

	zap.L().Debug("kubectl", zap.Strings("cli", arg))
	runErr := copyCmd.Run()
	if runErr != nil {
		zap.L().Error("kubectl",
			zap.Strings("args", arg),
			zap.ByteString("stderr", errbuf.Bytes()),
			zap.ByteString("stdout", outbuf.Bytes()),
			zap.Error(runErr),
		)
		return runErr
	}

	untar := strings.Split(strings.Trim(outbuf.String(), "\n"), "\n")
	zap.L().Debug("copied", zap.Strings("untar", untar))

	return nil
}
