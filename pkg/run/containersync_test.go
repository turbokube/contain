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

	stateRunning := run.RunpodContainerStatusState{
		Running: run.RunpodContainerStatusStateRunning{
			StartedAt: "2023-03-26T08:51:14Z",
		},
	}
	stateWaiting := run.RunpodContainerStatusState{
		Waiting: run.RunpodContainerStatusStateWaiting{
			Reason: "ContainerCreating",
		},
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Pending",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1",
					State: stateWaiting,
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
					Image: "busybox:1",
					State: stateRunning,
				},
			},
		},
	}) == nil {
		t.Errorf("Should accept running")
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Succeeded",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1",
					State: stateRunning,
				},
			},
		},
	}) != nil {
		t.Errorf("Should disregard container status if pod phase isn't Running")
	}

	if sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "alpine:1",
					State: stateRunning,
				},
			},
		},
	}) != nil {
		t.Errorf("should reject mismatching base image")
	}

	exact := run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1",
					State: stateRunning,
				},
			},
		},
	}
	matchContainerExact := sync.MatchPod(exact)
	if matchContainerExact == nil {
		t.Errorf("should accept running + container running base image")
	}

	matchContainerSha := sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "busybox:1@sha256:b5d6fe0712636ceb7430189de28819e195e8966372edfc2d9409d79402a0dc16",
					State: stateRunning,
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
					State: stateRunning,
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
					State: stateRunning,
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
					State: stateRunning,
				},
				{
					Image: "busybox:1.36",
					State: stateRunning,
				},
			},
		},
	}) != nil {
		t.Errorf("should reject multiple containers match the base")
	}

	matchContainer2 := sync.MatchPod(run.Runpod{
		Status: run.RunpodStatus{
			Phase: "Running",
			ContainerStatuses: []run.RunpodContainerStatus{
				{
					Image: "idlybox:1.35",
					State: stateRunning,
				},
				{
					Image: "busybox:1.35",
					State: stateRunning,
				},
				{
					Image: "lazybox:1.35",
					State: stateRunning,
				},
			},
		},
	})
	if matchContainer2.Image != "busybox:1.35" {
		t.Errorf("should return the matching container among many, got %s", matchContainer2.Image)
	}

}
