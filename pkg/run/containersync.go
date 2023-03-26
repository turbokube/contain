package run

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

type Containersync struct {
	config *schema.ContainConfig
}

type SyncTarget struct {
	pod       RunpodMetadata
	container RunpodContainerStatus
}

func NewContainersync(config *schema.ContainConfig) (*Containersync, error) {
	return &Containersync{
		config: config,
	}, nil
}

func (c *Containersync) Run(layers ...v1.Layer) error {
	if len(layers) != 1 {
		return fmt.Errorf("only single layer sync is supported at the momemnt, got %d", len(layers))
	}
	target, err := c.PodWait(1)
	if err != nil {
		zap.L().Fatal("failed to get sync target pod",
			zap.Error(err),
			zap.String("namespace", c.config.Sync.Namespace),
			zap.String("selector", c.config.Sync.PodSelector),
			zap.String("base", c.config.Base),
		)
	}
	zap.L().Info("sync pod",
		zap.String("namespace", target.pod.Namespace),
		zap.String("name", target.pod.Name),
		zap.String("created", target.pod.CreatedTimestamp),
		zap.String("container", target.container.Name),
		zap.String("image", target.container.Image),
	)

	for i, layer := range layers {
		zap.L().Debug("start sync", zap.Int("layer", i))
		if err := LayerToContainer(layer, target); err != nil {
			zap.L().Fatal("sync failed", zap.Int("layer", i))
		}
	}

	return nil
}

// MatchPod assumes that a selector was applied at get,
// and matches on pod status if the pod is a suitable sync target or not
func (c *Containersync) MatchPod(pod Runpod) *RunpodContainerStatus {
	if pod.Status.Phase != "Running" {
		zap.L().Debug("not running",
			zap.String("n", pod.Metadata.Namespace),
			zap.String("name", pod.Metadata.Name),
			zap.String("phase", pod.Status.Phase),
		)
	}
	var container *RunpodContainerStatus
	for i, cs := range pod.Status.ContainerStatuses {
		if strings.HasPrefix(cs.Image, c.config.Base) {
			zap.L().Debug("container match",
				zap.String("n", pod.Metadata.Namespace),
				zap.String("name", pod.Metadata.Name),
				zap.String("container", cs.Name),
				zap.Int("i", i),
			)
			if container != nil {
				zap.L().Error("multiple containers match", zap.String("previous", container.Name))
				return nil
			}
			container = &cs
		}
	}
	return container
}

func (c *Containersync) PodWait(attempt int) (*SyncTarget, error) {
	if attempt > c.config.Sync.GetAttemptsMax {
		return nil, fmt.Errorf("get sync pod max retries %d", c.config.Sync.GetAttemptsMax)
	}

	pods, err := PodInfo(c.config.Sync)
	if err != nil {
		// is there any get error category that warrants retry?
		zap.L().Fatal("get candidate pods for sync", zap.Error(err))
	}

	var target *SyncTarget
	for _, pod := range pods {
		container := c.MatchPod(pod)
		if container != nil {
			if target != nil {
				return nil, fmt.Errorf("more than one target pod: %s and %s", target.pod.Name, pod.Metadata.Name)
			}
			target = &SyncTarget{
				pod:       pod.Metadata,
				container: *container,
			}
		}
	}

	if target == nil {
		zap.L().Info("no matching target pod",
			zap.Int("retry", attempt),
			zap.Duration("wait", c.config.Sync.GetAttemptsWait),
		)
		time.Sleep(c.config.Sync.GetAttemptsWait)
		c.PodWait(attempt + 1)
	}

	return target, nil
}
