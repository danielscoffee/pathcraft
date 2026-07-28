package zaplogger

import (
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"go.uber.org/zap"
)

type Plugin struct{}

func (Plugin) Name() string { return "zap" }

func (Plugin) Logger() (*zap.Logger, error) { return zap.NewDevelopment() }

func init() { plugins.MustRegisterLogger(Plugin{}) }
