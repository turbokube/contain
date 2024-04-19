package run

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"

	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

type podList struct {
	Items []Runpod `json:"items"`
}

type Runpod struct {
	Metadata RunpodMetadata `json:"metadata"`
	Status   RunpodStatus   `json:"status"`
}

type RunpodMetadata struct {
	Name             string `json:"name"`
	Namespace        string `json:"namespace"`
	CreatedTimestamp string `json:"creationTimestamp"`
}

type RunpodStatus struct {
	Phase             string                  `json:"phase"`
	ContainerStatuses []RunpodContainerStatus `json:"containerStatuses"`
}

type RunpodContainerStatusState struct {
	Waiting    RunpodContainerStatusStateWaiting    `json:"waiting"`
	Running    RunpodContainerStatusStateRunning    `json:"running"`
	Terminated RunpodContainerStatusStateTerminated `json:"terminated"`
}

type RunpodContainerStatusStateWaiting struct {
	Reason string `json:"reason"`
}

type RunpodContainerStatusStateRunning struct {
	StartedAt string `json:"startedAt"`
}

type RunpodContainerStatusStateTerminated struct {
	ExitCode   int    `json:"exitCode"`
	FinishedAt string `json:"finishedAt"`
	Reason     string `json:"reason"` // for example "Completed"
	StartedAt  string `json:"startedAt"`
}

type RunpodContainerStatus struct {
	Name         string                     `json:"name"`
	Image        string                     `json:"imageID"`
	Ready        bool                       `json:"ready"`
	RestartCount int                        `json:"restartCount"`
	Started      bool                       `json:"started"`
	State        RunpodContainerStatusState `json:"state"`
}

func PodInfo(config schema.ContainConfigSync) ([]Runpod, error) {
	if config.PodSelector == "" {
		zap.L().Fatal("pod selector is required")
	}

	arg := []string{
		"get", "pods",
		"-o", "json",
		"--selector", config.PodSelector,
	}
	if config.Namespace != "" {
		arg = append(arg, "-n", config.Namespace)
	}
	addEnv := []string{}
	cmd := exec.Command("kubectl", arg...)
	cmd.Env = append(os.Environ(), addEnv...)
	var outbuf, errbuf bytes.Buffer
	cmd.Stdout = &outbuf
	cmd.Stderr = &errbuf

	zap.L().Debug("kubectl", zap.Strings("cli", arg))
	runErr := cmd.Run()
	if runErr != nil {
		zap.L().Error("kubectl",
			zap.Strings("args", arg),
			zap.ByteString("stderr", errbuf.Bytes()),
			zap.Error(runErr),
		)
		return nil, runErr
	}

	var podlist podList

	json.Unmarshal(outbuf.Bytes(), &podlist)
	return podlist.Items, nil
}
