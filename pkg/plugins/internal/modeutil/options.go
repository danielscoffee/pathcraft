package modeutil

import (
	"fmt"
	"math"
	"strconv"
)

func PositiveOption(options map[string]string, name string, fallback float64) (float64, error) {
	value := options[name]
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%s must be a positive finite number", name)
	}
	return parsed, nil
}
