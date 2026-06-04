package ui

import (
	"testing"
)

func TestSpringConverge(t *testing.T) {
	s := Spring{K: 80, C: 5, Target: 0.96, Pos: 1.0}
	for i := 0; i < 200; i++ {
		s.Integrate(0.008)
	}
	if s.Pos < 0.955 || s.Pos > 0.965 {
		t.Errorf("spring should converge near target 0.96, got %.4f", s.Pos)
	}
	if s.Vel*s.Vel > 1e-4 {
		t.Errorf("spring velocity should settle near 0, got %.6f", s.Vel)
	}
}

// 松手过冲：欠阻尼弹簧初始速度应产生明显过冲
func TestSpringOvershoot(t *testing.T) {
	s := Spring{K: 80, C: 5, Target: 1.06, Pos: 1.06, Vel: 0.1}
	peak := s.Pos
	for i := 0; i < 300; i++ {
		s.Integrate(0.008)
		if s.Pos > peak {
			peak = s.Pos
		}
	}
	if peak <= 1.065 {
		t.Errorf("spring with initial velocity should overshoot target; target=1.06, peak=%.4f", peak)
	}
}
