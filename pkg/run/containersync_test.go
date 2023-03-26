package run_test

import (
	"testing"

	"github.com/turbokube/contain/pkg/run"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestMatchPod(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	config := schema.ContainConfig{
		Base: "busybox:1",
	}
	sync, err := run.NewContainersync(&config)
	if err != nil {
		t.Fail()
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Succeeded",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox",
				},
			},
		},
	}) != nil {
		t.Errorf("should reject not running")
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "alpine",
				},
			},
		},
	}) != nil {
		t.Errorf("should reject mismatching base image")
	}

	matchContainerExact := sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1",
				},
			},
		},
	})
	if matchContainerExact == nil {
		t.Errorf("should accept running + container running base image")
	}

	matchContainerSha := sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1@sha256:b5d6fe0712636ceb7430189de28819e195e8966372edfc2d9409d79402a0dc16",
				},
			},
		},
	})
	if matchContainerSha == nil {
		t.Errorf("should accept running + container matching base but with sha")
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox@sha256:b5d6fe0712636ceb7430189de28819e195e8966372edfc2d9409d79402a0dc16",
				},
			},
		},
	}) != nil {
		t.Errorf("should reject when base has tag and running image hasn't")
	}

	matchContainerTag := sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1.36",
				},
			},
		},
	})
	if matchContainerTag == nil {
		t.Errorf("should accept running + container matching base as prefix")
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1.35",
				},
				{
					Image: "busybox:1.36",
				},
			},
		},
	}) != nil {
		t.Errorf("should reject multiple containers match the base")
	}

}
