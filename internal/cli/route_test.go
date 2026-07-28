package cli

import (
	"reflect"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

func TestParseCLIPositionPreservesDimensions(t *testing.T) {
	got, err := parseCLIPosition("from", "1.5, 2.5, 300")
	if err != nil {
		t.Fatal(err)
	}
	want := core.Position{1.5, 2.5, 300}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("position = %v, want %v", got, want)
	}
}
