package http

import (
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestToJourneyResponsePreservesEveryLegTiming(t *testing.T) {
	legs := []engine.JourneyLeg{
		{Mode: "walk", DepartureTime: "08:00:00", ArrivalTime: "08:02:00", Duration: 2 * time.Minute},
		{Mode: "transit", DepartureTime: "08:03:00", ArrivalTime: "08:13:00", Duration: 10 * time.Minute},
		{Mode: "transfer", DepartureTime: "08:13:00", ArrivalTime: "08:15:00", Duration: 2 * time.Minute},
		{Mode: "walk", DepartureTime: "08:15:00", ArrivalTime: "08:18:00", Duration: 3 * time.Minute},
	}
	response := toJourneyResponse(&engine.MultimodalRouteResult{
		Mode:          "multimodal",
		DepartureTime: "08:00:00",
		ArrivalTime:   "08:18:00",
		Legs:          legs,
		TransitPath:   legs[1:3],
	})

	if len(response.Legs) != len(legs) || len(response.TransitPath) != 2 {
		t.Fatalf("response legs = %d/%d, want %d/2", len(response.Legs), len(response.TransitPath), len(legs))
	}
	for i, leg := range response.Legs {
		if leg.DepartureTime != legs[i].DepartureTime || leg.ArrivalTime != legs[i].ArrivalTime {
			t.Fatalf("leg %d timing = %s-%s, want %s-%s", i, leg.DepartureTime, leg.ArrivalTime, legs[i].DepartureTime, legs[i].ArrivalTime)
		}
		if leg.DurationSeconds != int64(legs[i].Duration/time.Second) {
			t.Fatalf("leg %d duration = %d, want %d", i, leg.DurationSeconds, int64(legs[i].Duration/time.Second))
		}
	}
}
