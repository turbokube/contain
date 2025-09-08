package pushed

import (
	"regexp"
	"strings"
	"time"
)

var (
	defaultEnv = regexp.MustCompile(`^(CI|CI_.*|TURBO_.*|IMAGE|IMAGE_.*)$`)
)

type BuildTrace struct {
	Start *time.Time        `json:"start,omitempty"`
	End   *time.Time        `json:"end,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
}

func BuildTraceEnv(environ []string) map[string]string {
	env := make(map[string]string)
	for _, e := range environ {
		pair := strings.SplitN(e, "=", 2)
		if defaultEnv.MatchString(pair[0]) {
			env[pair[0]] = pair[1]
		}
	}
	return env
}
