package core

import "go.uber.org/zap"

// LoggerPlugin provides a structured application logger.
type LoggerPlugin interface {
	Name() string
	Logger() (*zap.Logger, error)
}
