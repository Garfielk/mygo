package ui

import (
	"fmt"
	"testing"
)

// TestTwoTestersInParallel runs Testers on different goroutines, as
// t.Parallel does, and fails under go test -race while they share one
// glyph atlas (issue #1722).
func TestTwoTestersInParallel(t *testing.T) {
	for i := range 4 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			frame := 0
			tt := NewTester(func(c *Context) {
				Column(c).Children(func() {
					for j := range 30 {
						r := rune(0x4e00 + i*3000 + frame*30 + j) // new glyphs every frame
						Row(c).Children(func() {
							Text(c, fmt.Sprintf("tester %d frame %d %c", i, frame, r)).FontSize(float32(11 + (frame+j)%9))
						})
					}
				})
			}, 400, 900)
			for range 40 {
				frame++
				tt.Frame()
			}
		})
	}
}
