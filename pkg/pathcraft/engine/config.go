package engine

import (
	"fmt"
	"math"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/mobility"
)

type Config struct {
	SpeedMPS         float64
	HighwayPenalties map[string]float64
}

func DefaultConfig() Config {
	return Config{SpeedMPS: mobility.DefaultWalkingSpeedMPS}
}

func NewWithConfig(config Config) (*Engine, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	return &Engine{config: normalized}, nil
}

func normalizeConfig(config Config) (Config, error) {
	if config.SpeedMPS < 0 || math.IsNaN(config.SpeedMPS) || math.IsInf(config.SpeedMPS, 0) {
		return Config{}, fmt.Errorf("speed must be finite and non-negative")
	}
	if config.SpeedMPS == 0 {
		config.SpeedMPS = mobility.DefaultWalkingSpeedMPS
	}

	penalties := make(map[string]float64, len(config.HighwayPenalties))
	for highway, penalty := range config.HighwayPenalties {
		highway = strings.ToLower(strings.TrimSpace(highway))
		if highway == "" {
			return Config{}, fmt.Errorf("highway penalty name must not be empty")
		}
		if penalty < 1 || math.IsNaN(penalty) || math.IsInf(penalty, 0) {
			return Config{}, fmt.Errorf("highway penalty for %q must be finite and at least 1", highway)
		}
		if _, exists := penalties[highway]; exists {
			return Config{}, fmt.Errorf("duplicate highway penalty %q", highway)
		}
		penalties[highway] = penalty
	}
	config.HighwayPenalties = penalties
	return config, nil
}

func (config Config) profile() mobility.Profile {
	return mobility.NewWalking(config.SpeedMPS)
}

type configuredProfile struct {
	mobility.Profile
	penalties map[string]float64
}

func (profile configuredProfile) HighwayPenalty(highway string) float64 {
	if penalty, ok := profile.penalties[strings.ToLower(strings.TrimSpace(highway))]; ok {
		return penalty
	}
	return 1
}

func (configuredProfile) HighwayPenaltyLowerBound() float64 {
	return 1
}

func (e *Engine) routeProfile(profile mobility.Profile) mobility.Profile {
	config := e.config
	if config.SpeedMPS == 0 {
		config = DefaultConfig()
	}
	if profile == nil {
		profile = config.profile()
	}
	if len(config.HighwayPenalties) == 0 {
		return profile
	}
	return configuredProfile{Profile: profile, penalties: config.HighwayPenalties}
}
