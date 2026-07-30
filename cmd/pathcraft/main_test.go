package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunDispatchesChunksCommand(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"pathcraft", "chunks"}
	t.Cleanup(func() { os.Args = originalArgs })

	err := run()
	if err == nil || !strings.Contains(err.Error(), "build subcommand") {
		t.Fatalf("run() error = %v, want chunks validation", err)
	}
}

func TestRunDispatchesGlobalChunksCommand(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"pathcraft", "chunks", "build-global"}
	t.Cleanup(func() { os.Args = originalArgs })

	err := run()
	if err == nil || !strings.Contains(err.Error(), "--pbf is required") {
		t.Fatalf("run() error = %v, want global chunks validation", err)
	}
}

func TestRunDispatchesGRPCCommand(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"pathcraft", "grpc"}
	t.Cleanup(func() { os.Args = originalArgs })

	err := run()
	if err == nil || !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("run() error = %v, want gRPC file validation", err)
	}
}
