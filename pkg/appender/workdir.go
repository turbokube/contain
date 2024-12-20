package appender

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

type Chdir struct {
	pwd      string
	restored bool
}

// NewChdir changes current working directory to the path given by the dir arg
func NewChdir(dir string) *Chdir {
	if !filepath.IsAbs(dir) {
		zap.L().Fatal("should be absolute", zap.String("dir", dir))
	}
	pwd, err := os.Getwd()
	if err != nil {
		zap.L().Fatal("get cwd",
			zap.Error(err),
		)
	}
	if pwd == dir {
		zap.L().Warn("chdir change to current", zap.String("dir", dir))
	}
	err = os.Chdir(dir)
	if err != nil {
		zap.L().Fatal("change cwd",
			zap.String("dir", dir),
			zap.Error(err),
		)
	}
	zap.L().Debug("cwd changed",
		zap.String("to", dir),
		zap.String("from", pwd),
	)
	return &Chdir{
		pwd: pwd,
	}
}

// cleanup restores working directory based on the result of NewChdir
func (c *Chdir) Cleanup() {
	if c.restored {
		return
	}
	err := os.Chdir(c.pwd)
	if err != nil {
		zap.L().Fatal("restore cwd",
			zap.String("to", c.pwd),
			zap.Error(err),
		)
	}
	zap.L().Debug("cwd restored",
		zap.String("dir", c.pwd),
	)
	c.restored = true
}
