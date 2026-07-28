package cli

import (
	"slices"
	"testing"
)

func TestDefaultHTTPAddressIsLoopback(t *testing.T) {
	if defaultHTTPAddress != "127.0.0.1:8080" {
		t.Fatalf("default HTTP address = %q", defaultHTTPAddress)
	}
}

func TestParseCORSOrigins(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: "", want: nil},
		{name: "one", value: "https://app.example", want: []string{"https://app.example"}},
		{name: "many", value: "https://a.example,https://b.example", want: []string{"https://a.example", "https://b.example"}},
		{name: "spaces", value: " https://a.example, , https://b.example ", want: []string{"https://a.example", "https://b.example"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseCORSOrigins(test.value); !slices.Equal(got, test.want) {
				t.Fatalf("parseCORSOrigins(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
