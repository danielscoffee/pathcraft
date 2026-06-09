package zaplogger

import (
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
	"go.uber.org/zap"
)

type Plugin struct{}

func (Plugin) Name() string { return "zap" }

func (Plugin) Logger() (*zap.Logger, error) { return zap.NewDevelopment() }

func init() { registry.MustRegisterLogger(Plugin{}) }
