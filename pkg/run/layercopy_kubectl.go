package run

import (
	"bytes"
	"os/exec"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
)

func LayerToContainer(layer v1.Layer, target *SyncTarget) error {
	// https://github.com/GoogleContainerTools/skaffold/blob/v2.2.0/pkg/skaffold/sync/kubectl.go#L49
	arg := []string{
		"exec", target.pod.Name,
		"--namespace", target.pod.Namespace,
		"-c", target.container.Name,
		"-i",
		"--",
		"tar",
		"xvzmf",
		"-",
		"-C",
		"/",
		"--no-same-owner",
	}

	copyCmd := exec.Command("kubectl", arg...)
	var outbuf, errbuf bytes.Buffer
	copyCmd.Stdout = &outbuf
	copyCmd.Stderr = &errbuf

	var err error
	copyCmd.Stdin, err = layer.Compressed()
	if err != nil {
		return err
	}

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
