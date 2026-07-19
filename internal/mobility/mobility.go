package mobility

type Profile interface {
	Name() string
	Speed() float64
}

type basicProfile struct {
	name  string
	speed float64
}

func (p basicProfile) Name() string   { return p.name }
func (p basicProfile) Speed() float64 { return p.speed }

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
