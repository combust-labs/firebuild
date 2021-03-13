package tracing

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
)

type adapter struct {
	log hclog.Logger
}

func (l adapter) Error(msg string) {
	l.log.Error(msg)
}

func (l adapter) Infof(msg string, args ...interface{}) {
	l.log.Info(fmt.Sprintf(msg, args...))
}
