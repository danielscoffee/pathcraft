package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunDispatchesGRPCCommand(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"pathcraft", "grpc"}
	t.Cleanup(func() { os.Args = originalArgs })

	err := run()
	if err == nil || !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("run() error = %v, want gRPC file validation", err)
	}
}
