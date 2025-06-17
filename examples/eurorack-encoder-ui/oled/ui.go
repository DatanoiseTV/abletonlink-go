package oled

import (
	"log"
	"time"
)

// UIManager coordinates the entire OLED user interface
type UIManager struct {
	display *Display
	menu    *Menu
	input   *InputHandler
	
	// Update control
	updateTicker *time.Ticker
	stopChan     chan bool
	
	// Data sync callbacks
	onTempoChange func(float64)
	onPhaseChange func(string, float64)
	onSwingChange func(string, float64)
	
	// Current state
	tempo      float64
	isPlaying  bool
	peerCount  int
	beatPhase  float64
}

// Config holds UI configuration
type Config struct {
	I2CBus      int
	I2CAddress  int
	DisplaySize DisplaySize
	EncoderA    int
	EncoderB    int
	ButtonPin   int
	BackPin     int
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		I2CBus:      1,
		I2CAddress:  0x3C,
		DisplaySize: Size128x32,
		EncoderA:    25,
		EncoderB:    26,
		ButtonPin:   27,
		BackPin:     5,
	}
}

// NewUIManager creates a new UI manager
func NewUIManager(config Config) (*UIManager, error) {
	// Initialize display
	display, err := NewDisplay(config.I2CBus, config.I2CAddress, config.DisplaySize)
	if err != nil {
		return nil, err
	}
	
	// Initialize menu system
	menu := NewMenu(display)
	
	// Initialize input handler
	input := NewInputHandler(config.EncoderA, config.EncoderB, config.ButtonPin, config.BackPin)
	if err := input.Initialize(); err != nil {
		return nil, err
	}
	
	ui := &UIManager{
		display:      display,
		menu:         menu,
		input:        input,
		updateTicker: time.NewTicker(33 * time.Millisecond), // 30 FPS
		stopChan:     make(chan bool),
	}
	
	// Set up menu callbacks
	menu.SetCallbacks(
		func(tempo float64) {
			if ui.onTempoChange != nil {
				ui.onTempoChange(tempo)
			}
		},
		func(output string, phase float64) {
			if ui.onPhaseChange != nil {
				ui.onPhaseChange(output, phase)
			}
		},
		func(output string, swing float64) {
			if ui.onSwingChange != nil {
				ui.onSwingChange(output, swing)
			}
		},
	)
	
	return ui, nil
}

// SetCallbacks sets the data update callbacks
func (ui *UIManager) SetCallbacks(onTempo func(float64), onPhase func(string, float64), onSwing func(string, float64)) {
	ui.onTempoChange = onTempo
	ui.onPhaseChange = onPhase
	ui.onSwingChange = onSwing
}

// UpdateData updates the live data shown in the UI
func (ui *UIManager) UpdateData(tempo float64, isPlaying bool, peerCount int, beatPhase float64) {
	ui.tempo = tempo
	ui.isPlaying = isPlaying
	ui.peerCount = peerCount
	ui.beatPhase = beatPhase
	
	// Update menu with latest data
	ui.menu.UpdateData(tempo, isPlaying, peerCount, beatPhase)
}

// Start begins the UI update loop
func (ui *UIManager) Start() {
	go ui.inputLoop()
	go ui.updateLoop()
}

// inputLoop processes input events
func (ui *UIManager) inputLoop() {
	events := ui.input.GetEvents()
	
	for {
		select {
		case event := <-events:
			ui.handleInputEvent(event)
		case <-ui.stopChan:
			return
		}
	}
}

// updateLoop handles regular UI updates and rendering
func (ui *UIManager) updateLoop() {
	for {
		select {
		case <-ui.updateTicker.C:
			ui.render()
		case <-ui.stopChan:
			return
		}
	}
}

// handleInputEvent processes input events and sends them to the menu
func (ui *UIManager) handleInputEvent(event InputEvent) {
	ui.menu.HandleInput(event.Type, event.Value)
}

// render draws the current UI state
func (ui *UIManager) render() {
	// Only render if not animating or if animation needs update
	if !ui.menu.IsAnimating() {
		// Check if we need to rebuild menu items based on data changes
		ui.menu.buildMenuItems()
	}
	
	// Render menu
	ui.menu.Render()
	
	// Update display
	if err := ui.display.Update(); err != nil {
		log.Printf("Display update error: %v", err)
	}
}

// Stop shuts down the UI manager
func (ui *UIManager) Stop() {
	ui.updateTicker.Stop()
	close(ui.stopChan)
	ui.input.Close()
	ui.display.Close()
}

// ShowSplashScreen displays a startup animation
func (ui *UIManager) ShowSplashScreen(duration time.Duration) {
	graphics := NewGraphics(ui.display)
	size := ui.display.Size()
	
	// Clear display
	ui.display.Clear()
	
	// Draw splash content
	title := "EURORACK LINK"
	subtitle := "v2.0"
	
	// Center title
	titleWidth := graphics.GetTextWidth(title)
	titleX := (size.Width - titleWidth) / 2
	titleY := size.Height/2 - 10
	
	graphics.DrawText(titleX, titleY, title, White)
	
	// Center subtitle
	subtitleWidth := graphics.GetTextWidth(subtitle)
	subtitleX := (size.Width - subtitleWidth) / 2
	subtitleY := titleY + 12
	
	graphics.DrawText(subtitleX, subtitleY, subtitle, Gray)
	
	// Animated progress bar
	ui.display.StartAnimation(duration, EaseOutQuad)
	
	start := time.Now()
	for time.Since(start) < duration {
		progress := float64(time.Since(start)) / float64(duration)
		
		// Clear progress area
		graphics.FillRect(10, size.Height-8, size.Width-20, 4, Black)
		
		// Draw progress bar
		graphics.DrawProgressBar(10, size.Height-8, size.Width-20, 4, progress, White, Gray)
		
		ui.display.Update()
		time.Sleep(33 * time.Millisecond) // 30 FPS
	}
	
	// Clear for normal operation
	ui.display.Clear()
	ui.display.Update()
}