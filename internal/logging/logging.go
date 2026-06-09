package logging

import (
	"fmt"
	"sync"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
	"go.uber.org/zap"
)

var (
	mu     sync.RWMutex
	logger = zap.NewNop()
)

func Init(name string) error {
	if name == "" {
		name = "zap"
	}
	plugin, ok := registry.Default.Logger(name)
	if !ok {
		return fmt.Errorf("unknown logger plugin: %s", name)
	}
	l, err := plugin.Logger()
	if err != nil {
		return err
	}
	SetLogger(l)
	return nil
}

func InitDevelopment() error { return Init("zap") }

func SetLogger(l *zap.Logger) {
	if l == nil {
		l = zap.NewNop()
	}
	mu.Lock()
	logger = l
	mu.Unlock()
}

func L() *zap.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

func Sync() {
	_ = L().Sync()
}
