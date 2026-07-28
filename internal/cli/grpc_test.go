package cli

import (
	"strings"
	"testing"
)

func TestParseGRPCArgsRequiresOSMFile(t *testing.T) {
	_, err := parseGRPCArgs(nil)
	if err == nil || !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("parseGRPCArgs() error = %v, want required file", err)
	}
}

func TestParseGRPCArgsDefaultsToLoopback(t *testing.T) {
	options, err := parseGRPCArgs([]string{"--file", "map.osm"})
	if err != nil {
		t.Fatalf("parseGRPCArgs() error = %v", err)
	}
	if options.file != "map.osm" || options.addr != "127.0.0.1:9090" || options.gtfsDir != "" {
		t.Fatalf("options = %+v", options)
	}
}

func TestParseGRPCArgsAcceptsGTFSAndAddress(t *testing.T) {
	options, err := parseGRPCArgs([]string{
		"--file", "map.osm",
		"--gtfs", "feed",
		"--addr", "127.0.0.1:19090",
	})
	if err != nil {
		t.Fatalf("parseGRPCArgs() error = %v", err)
	}
	if options.gtfsDir != "feed" || options.addr != "127.0.0.1:19090" {
		t.Fatalf("options = %+v", options)
	}
}

func TestParseGRPCArgsRejectsUnknownFlag(t *testing.T) {
	if _, err := parseGRPCArgs([]string{"--unknown"}); err == nil {
		t.Fatal("parseGRPCArgs() error = nil, want unknown flag error")
	}
}
