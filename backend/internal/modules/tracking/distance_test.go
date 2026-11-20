package tracking

import (
	"math"
	"testing"
)

func TestDistanceMeters(t *testing.T) {
	// Pune station to Shivajinagar is about 2.9 km; a point to itself is 0.
	if d := DistanceMeters(18.5286, 73.8743, 18.5308, 73.8475); math.Abs(d-2840) > 60 {
		t.Fatalf("distance = %.0f m", d)
	}
	if d := DistanceMeters(18.59, 73.74, 18.59, 73.74); d != 0 {
		t.Fatalf("same point = %f", d)
	}
	// 0.05 degrees in both directions near Pune is about 7.66 km (5.56 km north, 5.27 km east).
	if d := DistanceMeters(18.5913, 73.7389, 18.6413, 73.7889); math.Abs(d-7660) > 50 {
		t.Fatalf("diagonal = %.0f m", d)
	}
}
