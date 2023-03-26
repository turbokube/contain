package run

import (
	"errors"
	"fmt"
	"os/exec"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/contain"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Containersync struct {
	kubeconfig *rest.Config
}

func NewContainersync(config *contain.Contain) (*Containersync, error) {
	kubeconfig, err := kubeConfig()
	if err != nil {
		return nil, err
	}
	return &Containersync{
		kubeconfig: kubeconfig,
	}, nil
}

func (c *Containersync) Run(layers ...v1.Layer) error {
	if len(layers) != 1 {
		return fmt.Errorf("only single layer sync is supported at the momemnt, got %d", len(layers))
	}
	cmd := exec.Command("prog")
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}
	// cmd.Stdin
	// https://github.com/GoogleContainerTools/skaffold/blob/v2.2.0/pkg/skaffold/sync/kubectl.go#L49
	// copyCmd := s.kubectl.Command(ctx, "exec", pod.Name, "--namespace", pod.Namespace, "-c", container.Name, "-i", "--", "tar", "xmf", "-", "-C", "/", "--no-same-owner")
	// copyCmd.Stdin = reader

	return nil
}

func kubeConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	config.Na
	return config, err
}

func (c *Containersync) Pod() error {
	clientset, err := kubernetes.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}
	clientset.CoreV1().Pods()
	return nil
}
