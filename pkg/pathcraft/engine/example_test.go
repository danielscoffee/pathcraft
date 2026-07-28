package engine_test

import (
	"fmt"
	"os"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func ExampleEngine_LoadOSMReader() {
	osmFile, err := os.Open("../../../testdata/example.osm")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer osmFile.Close()

	e := engine.New()
	if err := e.LoadOSMReader(osmFile); err != nil {
		fmt.Println(err)
		return
	}
	route, err := e.Route(engine.RouteRequest{From: 1, To: 6})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(route.Nodes)
	fmt.Println(e.Stats().Nodes)
	// Output:
	// [1 2 3 4 5 6]
	// 6
}
