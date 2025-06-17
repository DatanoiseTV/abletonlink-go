package oled

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Colors
var (
	White = color.RGBA{255, 255, 255, 255}
	Black = color.RGBA{0, 0, 0, 255}
	Gray  = color.RGBA{128, 128, 128, 255}
)

// Graphics provides advanced drawing functions
type Graphics struct {
	display *Display
	font    font.Face
}

// NewGraphics creates a new graphics context
func NewGraphics(display *Display) *Graphics {
	return &Graphics{
		display: display,
		font:    basicfont.Face7x13,
	}
}

// FillRect draws a filled rectangle
func (g *Graphics) FillRect(x, y, width, height int, col color.RGBA) {
	buffer := g.display.Buffer()
	
	for py := y; py < y+height && py < g.display.size.Height; py++ {
		for px := x; px < x+width && px < g.display.size.Width; px++ {
			if px >= 0 && py >= 0 {
				buffer.Set(px, py, col)
			}
		}
	}
	g.display.MarkDirty()
}

// DrawRect draws a rectangle outline
func (g *Graphics) DrawRect(x, y, width, height int, col color.RGBA) {
	g.DrawHLine(x, y, width, col)           // Top
	g.DrawHLine(x, y+height-1, width, col)  // Bottom
	g.DrawVLine(x, y, height, col)          // Left
	g.DrawVLine(x+width-1, y, height, col)  // Right
}

// DrawHLine draws a horizontal line
func (g *Graphics) DrawHLine(x, y, width int, col color.RGBA) {
	buffer := g.display.Buffer()
	
	for px := x; px < x+width && px < g.display.size.Width; px++ {
		if px >= 0 && y >= 0 && y < g.display.size.Height {
			buffer.Set(px, y, col)
		}
	}
	g.display.MarkDirty()
}

// DrawVLine draws a vertical line
func (g *Graphics) DrawVLine(x, y, height int, col color.RGBA) {
	buffer := g.display.Buffer()
	
	for py := y; py < y+height && py < g.display.size.Height; py++ {
		if x >= 0 && py >= 0 && x < g.display.size.Width {
			buffer.Set(x, py, col)
		}
	}
	g.display.MarkDirty()
}

// DrawCircle draws a circle outline
func (g *Graphics) DrawCircle(centerX, centerY, radius int, col color.RGBA) {
	buffer := g.display.Buffer()
	
	// Bresenham's circle algorithm
	x := radius
	y := 0
	err := 0
	
	for x >= y {
		// Draw 8 octants
		points := [][2]int{
			{centerX + x, centerY + y}, {centerX + y, centerY + x},
			{centerX - y, centerY + x}, {centerX - x, centerY + y},
			{centerX - x, centerY - y}, {centerX - y, centerY - x},
			{centerX + y, centerY - x}, {centerX + x, centerY - y},
		}
		
		for _, p := range points {
			if p[0] >= 0 && p[0] < g.display.size.Width && p[1] >= 0 && p[1] < g.display.size.Height {
				buffer.Set(p[0], p[1], col)
			}
		}
		
		if err <= 0 {
			y++
			err += 2*y + 1
		}
		if err > 0 {
			x--
			err -= 2*x + 1
		}
	}
	g.display.MarkDirty()
}

// FillCircle draws a filled circle
func (g *Graphics) FillCircle(centerX, centerY, radius int, col color.RGBA) {
	buffer := g.display.Buffer()
	
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				px, py := centerX+x, centerY+y
				if px >= 0 && px < g.display.size.Width && py >= 0 && py < g.display.size.Height {
					buffer.Set(px, py, col)
				}
			}
		}
	}
	g.display.MarkDirty()
}

// DrawText draws text at the specified position
func (g *Graphics) DrawText(x, y int, text string, col color.RGBA) {
	drawer := &font.Drawer{
		Dst:  g.display.Buffer(),
		Src:  &image.Uniform{col},
		Face: g.font,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y + 10)}, // Adjust Y for baseline
	}
	drawer.DrawString(text)
	g.display.MarkDirty()
}

// DrawTextCentered draws text centered horizontally
func (g *Graphics) DrawTextCentered(y int, text string, col color.RGBA) {
	width := g.GetTextWidth(text)
	x := (g.display.size.Width - width) / 2
	g.DrawText(x, y, text, col)
}

// DrawTextRight draws text aligned to the right
func (g *Graphics) DrawTextRight(x, y int, text string, col color.RGBA) {
	width := g.GetTextWidth(text)
	g.DrawText(x-width, y, text, col)
}

// GetTextWidth returns the width of text in pixels
func (g *Graphics) GetTextWidth(text string) int {
	bounds, _ := font.BoundString(g.font, text)
	return int(bounds.Max.X-bounds.Min.X) >> 6
}

// GetTextHeight returns the height of text in pixels
func (g *Graphics) GetTextHeight() int {
	return 13 // basicfont.Face7x13 height
}

// DrawProgressBar draws an animated progress bar
func (g *Graphics) DrawProgressBar(x, y, width, height int, progress float64, filled, empty color.RGBA) {
	// Draw background
	g.FillRect(x, y, width, height, empty)
	
	// Draw filled portion
	filledWidth := int(float64(width) * progress)
	if filledWidth > 0 {
		g.FillRect(x, y, filledWidth, height, filled)
	}
	
	// Draw border
	g.DrawRect(x, y, width, height, White)
}

// DrawScrollIndicator draws a scroll position indicator
func (g *Graphics) DrawScrollIndicator(x, y, height, totalItems, visibleItems, currentPosition int) {
	if totalItems <= visibleItems {
		return // No scrolling needed
	}
	
	// Calculate indicator size and position
	indicatorHeight := (height * visibleItems) / totalItems
	if indicatorHeight < 3 {
		indicatorHeight = 3 // Minimum size
	}
	
	indicatorY := y + (currentPosition * (height - indicatorHeight)) / (totalItems - visibleItems)
	
	// Draw track
	g.DrawVLine(x, y, height, Gray)
	
	// Draw indicator
	g.FillRect(x-1, indicatorY, 3, indicatorHeight, White)
}

// DrawBeatIndicator draws an animated beat visualization
func (g *Graphics) DrawBeatIndicator(x, y, size int, phase float64) {
	// Pulsing circle based on beat phase
	intensity := math.Sin(phase * 2 * math.Pi)
	if intensity < 0 {
		intensity = 0
	}
	
	radius := int(float64(size) * (0.3 + 0.7*intensity))
	
	// Draw outer ring
	g.DrawCircle(x, y, size, Gray)
	
	// Draw pulsing center
	if radius > 0 {
		g.FillCircle(x, y, radius, White)
	}
}

// DrawWaveform draws a simple waveform visualization
func (g *Graphics) DrawWaveform(x, y, width, height int, tempo float64) {
	// Generate a simple sine wave based on tempo
	frequency := tempo / 60.0 // Convert BPM to Hz
	
	buffer := g.display.Buffer()
	centerY := y + height/2
	
	for px := 0; px < width; px++ {
		// Calculate wave height
		t := float64(px) / float64(width) * 4 * math.Pi // 4 cycles across width
		wave := math.Sin(t * frequency)
		waveY := centerY + int(wave*float64(height/2)*0.8)
		
		// Draw wave point
		if x+px >= 0 && x+px < g.display.size.Width && waveY >= 0 && waveY < g.display.size.Height {
			buffer.Set(x+px, waveY, White)
		}
	}
	g.display.MarkDirty()
}

// DrawSlideTransition draws a slide transition effect
func (g *Graphics) DrawSlideTransition(progress float64, direction int, oldBuffer, newBuffer *image.RGBA) {
	buffer := g.display.Buffer()
	width := g.display.size.Width
	height := g.display.size.Height
	
	offset := int(float64(width) * progress)
	
	// Clear current buffer
	draw.Draw(buffer, buffer.Bounds(), &image.Uniform{Black}, image.Point{}, draw.Src)
	
	if direction > 0 {
		// Slide left (new content comes from right)
		// Draw old content sliding out to the left
		if offset < width {
			draw.Draw(buffer, image.Rect(0, 0, width-offset, height),
				oldBuffer, image.Point{offset, 0}, draw.Src)
		}
		// Draw new content sliding in from the right
		if offset > 0 {
			draw.Draw(buffer, image.Rect(width-offset, 0, width, height),
				newBuffer, image.Point{0, 0}, draw.Src)
		}
	} else {
		// Slide right (new content comes from left)
		// Draw old content sliding out to the right
		if offset < width {
			draw.Draw(buffer, image.Rect(offset, 0, width, height),
				oldBuffer, image.Point{0, 0}, draw.Src)
		}
		// Draw new content sliding in from the left
		if offset > 0 {
			draw.Draw(buffer, image.Rect(0, 0, offset, height),
				newBuffer, image.Point{width-offset, 0}, draw.Src)
		}
	}
	
	g.display.MarkDirty()
}

// DrawFadeTransition draws a fade transition effect
func (g *Graphics) DrawFadeTransition(progress float64, oldBuffer, newBuffer *image.RGBA) {
	buffer := g.display.Buffer()
	bounds := buffer.Bounds()
	
	// Alpha blending between old and new content
	alpha := uint8(progress * 255)
	
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			oldR, oldG, oldB, _ := oldBuffer.At(x, y).RGBA()
			newR, newG, newB, _ := newBuffer.At(x, y).RGBA()
			
			// Blend colors
			r := uint8((oldR*(255-uint32(alpha)) + newR*uint32(alpha)) / 255 / 256)
			g := uint8((oldG*(255-uint32(alpha)) + newG*uint32(alpha)) / 255 / 256)
			b := uint8((newB*(255-uint32(alpha)) + newB*uint32(alpha)) / 255 / 256)
			
			buffer.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	
	g.display.MarkDirty()
}