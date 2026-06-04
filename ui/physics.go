package ui

// Spring is a second-order mass-spring-damper with unit mass.
// K = stiffness (higher = faster snap), C = damping (lower = more bounce).
// Target is the equilibrium; Pos/Vel evolve each frame via Integrate.
// All values are in scale space (1.0 = original size).
type Spring struct {
	K, C   float32
	Target float32
	Pos    float32
	Vel    float32
}

// Integrate advances the spring by dt seconds (Euler integration).
// Called from render thread every frame.
func (s *Spring) Integrate(dt float32) {
	// ax = -k*(x-target) - c*vx  (restoring force + damping)
	a := s.K*(s.Target-s.Pos) - s.C*s.Vel
	s.Vel += a * dt
	s.Pos += s.Vel * dt
}
