package logging

import (
	"testing"

	"go.uber.org/zap"
)

func TestSetLoggerHandlesNil(t *testing.T) {
	SetLogger(nil)
	if L() == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestSetLoggerUsesProvidedLogger(t *testing.T) {
	logger := zap.NewNop()
	SetLogger(logger)
	if L() != logger {
		t.Fatal("expected provided logger")
	}
}
