package mobility

import (
	"fmt"
	"sort"
)

type Profile interface {
	Name() string
	Speed() float64
	TravelTime(distanceMeters float64) float64
}

type basicProfile struct {
	name  string
	speed float64
}

func (p basicProfile) Name() string   { return p.name }
func (p basicProfile) Speed() float64 { return p.speed }
func (p basicProfile) TravelTime(dist float64) float64 {
	if p.speed <= 0 {
		return 0
	}
	return dist / p.speed
}

type Factory func(speed float64) Profile

var registry = map[string]Factory{}

func Register(name string, factory Factory) {
	if name == "" {
		panic("profile name cannot be empty")
	}
	if factory == nil {
		panic("profile factory cannot be nil")
	}
	if _, exists := registry[name]; exists {
		panic("profile already registered: " + name)
	}

	registry[name] = factory
}

func New(name string, speed float64) (Profile, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown profile: %s", name)
	}
	return factory(speed), nil
}

func Available() []string {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func NewWalking(speed float64) Profile {
	if speed <= 0 {
		speed = DefaultWalkingSpeedMPS
	}
	return basicProfile{name: "walking", speed: speed}
}

func NewDriving(speed float64) Profile {
	if speed <= 0 {
		speed = DefaultDrivingSpeedMPS
	}
	return basicProfile{name: "driving", speed: speed}
}

func init() {
	Register("walking", NewWalking)
	Register("driving", NewDriving)
}
