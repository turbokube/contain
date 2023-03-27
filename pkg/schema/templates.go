package schema

import (
	"os"
	"time"

	v1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// TagFromEnv gets target ref from skaffold custom build invocation
func TagFromEnv() string {
	image, exists := os.LookupEnv("IMAGE")
	if exists {
		zap.L().Debug("IMAGE env found", zap.String("value", image))
	} else {
		return ""
	}
	return image
}

func IgnoreDefault() []string {
	return []string{
		"*Dockerfile",
		"*.dockerignore",
		"contain.yaml",
	}
}

func TemplateApp(base string) v1.ContainConfig {
	return v1.ContainConfig{
		Status: v1.ContainConfigStatus{
			Template: true,
		},
		Base: base,
		Tag:  TagFromEnv(),
		Layers: []v1.Layer{
			{
				LocalDir: v1.LocalDir{
					Path:          ".",
					ContainerPath: "/app",
					Ignore:        IgnoreDefault(),
					MaxFiles:      100,
					MaxSize:       "104857600", // "100Mi"
				},
			},
		},
	}
}

func TemplateSync(runNamespace string, runSelector string) v1.ContainConfigSync {
	defaultWait, err := time.ParseDuration("3s")
	if err != nil {
		zap.L().Fatal("parse default duration", zap.Error(err))
	}
	return v1.ContainConfigSync{
		Namespace:       runNamespace,
		PodSelector:     runSelector,
		GetAttemptsMax:  20,
		GetAttemptsWait: defaultWait,
	}
}
