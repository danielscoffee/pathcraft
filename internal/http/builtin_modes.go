package http

// Standard server exposes in-tree modes. Custom servers can supply an isolated registry.
import (
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/air"
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/bike"
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/car"
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/gtfsmode"
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/walk"
)
