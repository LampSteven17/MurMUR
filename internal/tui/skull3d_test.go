package tui

import (
	"fmt"
	"math"
	"testing"
)

// TestSkull3DVisual prints a few skull frames to stdout for manual inspection.
// Run with: go test -v -run TestSkull3DVisual ./internal/tui/
func TestSkull3DVisual(t *testing.T) {
	cols, rows := 60, 28
	angles := []float64{0, math.Pi / 4, math.Pi / 2, math.Pi}
	for i, yaw := range angles {
		fmt.Printf("\n--- frame %d yaw=%.2f rad (%.0f°) ---\n", i, yaw, yaw*180/math.Pi)
		fmt.Println(Skull3DFrame(yaw, cols, rows))
	}
}
