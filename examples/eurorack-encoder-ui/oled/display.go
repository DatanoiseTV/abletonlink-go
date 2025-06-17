package oled

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"time"

	"github.com/waxdred/go-i2c-oled"
	"github.com/waxdred/go-i2c-oled/ssd1306"
)

// DisplaySize represents the OLED dimensions
type DisplaySize struct {
	Width  int
	Height int
}

var (
	Size128x64 = DisplaySize{128, 64}
	Size128x32 = DisplaySize{128, 32}
)

// Display manages the OLED hardware and rendering
type Display struct {
	device *goi2coled.I2c
	buffer *image.RGBA
	size   DisplaySize
	
	// Animation state
	animMutex      sync.RWMutex
	animStartTime  time.Time
	animDuration   time.Duration
	animEasing     EasingFunc
	isAnimating    bool
	
	// Rendering
	needsUpdate    bool
	updateMutex    sync.RWMutex
}

// EasingFunc represents an animation easing function
type EasingFunc func(t float64) float64

// Common easing functions
var (
	EaseLinear    EasingFunc = func(t float64) float64 { return t }
	EaseInQuad    EasingFunc = func(t float64) float64 { return t * t }
	EaseOutQuad   EasingFunc = func(t float64) float64 { return 1 - (1-t)*(1-t) }
	EaseInOutQuad EasingFunc = func(t float64) float64 {
		if t < 0.5 {
			return 2 * t * t
		}
		return 1 - 2*(1-t)*(1-t)
	}
	EaseOutElastic EasingFunc = func(t float64) float64 {
		if t == 0 || t == 1 {
			return t
		}
		return math.Pow(2, -10*t) * math.Sin((t*10-0.75)*2*math.Pi/3) + 1
	}
)

// NewDisplay creates a new OLED display instance
func NewDisplay(busNum, address int, size DisplaySize) (*Display, error) {
	// Initialize the OLED device
	device, err := goi2coled.NewI2c(ssd1306.SSD1306_SWITCHCAPVCC, size.Height, size.Width, address, busNum)
	if err != nil {
		return nil, err
	}
	
	// Create display buffer
	buffer := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	
	d := &Display{
		device: device,
		buffer: buffer,
		size:   size,
		needsUpdate: true,
	}
	
	// Clear display
	d.Clear()
	d.Update()
	
	return d, nil
}

// Clear fills the display with black
func (d *Display) Clear() {
	d.updateMutex.Lock()
	defer d.updateMutex.Unlock()
	
	draw.Draw(d.buffer, d.buffer.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
	d.needsUpdate = true
}

// Update sends the buffer to the hardware display
func (d *Display) Update() error {
	d.updateMutex.RLock()
	needsUpdate := d.needsUpdate
	d.updateMutex.RUnlock()
	
	if !needsUpdate {
		return nil
	}
	
	// Send buffer to hardware
	d.device.DrawImage(d.buffer)
	err := d.device.Display()
	
	if err == nil {
		d.updateMutex.Lock()
		d.needsUpdate = false
		d.updateMutex.Unlock()
	}
	
	return err
}

// Size returns the display dimensions
func (d *Display) Size() DisplaySize {
	return d.size
}

// Buffer returns the image buffer for direct drawing
func (d *Display) Buffer() *image.RGBA {
	return d.buffer
}

// MarkDirty marks the display as needing an update
func (d *Display) MarkDirty() {
	d.updateMutex.Lock()
	d.needsUpdate = true
	d.updateMutex.Unlock()
}

// StartAnimation begins a new animation
func (d *Display) StartAnimation(duration time.Duration, easing EasingFunc) {
	d.animMutex.Lock()
	defer d.animMutex.Unlock()
	
	d.animStartTime = time.Now()
	d.animDuration = duration
	d.animEasing = easing
	d.isAnimating = true
}

// GetAnimationProgress returns the current animation progress (0.0 to 1.0)
func (d *Display) GetAnimationProgress() float64 {
	d.animMutex.RLock()
	defer d.animMutex.RUnlock()
	
	if !d.isAnimating {
		return 1.0
	}
	
	elapsed := time.Since(d.animStartTime)
	if elapsed >= d.animDuration {
		d.isAnimating = false
		return 1.0
	}
	
	progress := float64(elapsed) / float64(d.animDuration)
	if d.animEasing != nil {
		progress = d.animEasing(progress)
	}
	
	return progress
}

// IsAnimating returns true if an animation is currently running
func (d *Display) IsAnimating() bool {
	d.animMutex.RLock()
	defer d.animMutex.RUnlock()
	return d.isAnimating
}

// Close shuts down the display
func (d *Display) Close() error {
	d.Clear()
	d.Update()
	return nil
}