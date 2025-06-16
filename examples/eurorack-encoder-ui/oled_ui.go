package main

import (
	"fmt"
	"image"
	"image/draw"
	"time"

	"github.com/warthog618/go-gpiocdev"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/devices/v3/ssd1306"
	"periph.io/x/host/v3"
)

// Additional GPIO pins for encoder UI
type EncoderPins struct {
	EncoderA     int // Rotary encoder A pin
	EncoderB     int // Rotary encoder B pin
	EncoderBtn   int // Rotary encoder button
	BackBtn      int // Back button
	EnterBtn     int // Enter button
	CustomOut    int // Custom output pin
}

// Default encoder pin configuration
var defaultEncoderPins = EncoderPins{
	EncoderA:  25, // GPIO 25
	EncoderB:  26, // GPIO 26
	EncoderBtn: 27, // GPIO 27
	BackBtn:   5,  // GPIO 5
	EnterBtn:  6,  // GPIO 6
	CustomOut: 13, // GPIO 13
}

// Menu system states
type MenuState int

const (
	MenuMain MenuState = iota
	MenuTempo
	MenuCustomClock
	MenuPeers
	MenuStatus
	MenuSettings
)

// Custom clock divider options
type ClockDivider struct {
	Name     string
	Divider  float64 // Beats per pulse (e.g., 0.25 = 4 PPQN, 1.0 = 1 PPQN, 2.0 = 1 pulse per 2 beats)
}

var clockDividers = []ClockDivider{
	{"24 PPQN", 1.0 / 24.0},
	{"16 PPQN", 1.0 / 16.0},
	{"8 PPQN", 1.0 / 8.0},
	{"4 PPQN", 1.0 / 4.0},
	{"2 PPQN", 1.0 / 2.0},
	{"1 PPQN", 1.0},
	{"1/2 Note", 2.0},
	{"1/4 Note", 1.0},
	{"1/8 Note", 0.5},
	{"1/16 Note", 0.25},
	{"1/32 Note", 0.125},
}

// OLEDDisplay manages the OLED display and encoder interface
type OLEDDisplay struct {
	bridge    *EurorackLinkBridge
	display   *ssd1306.Dev
	pins      EncoderPins
	
	// Encoder state
	encoderLines map[string]*gpiocdev.Line
	encoderStateA int
	encoderStateB int
	encoderPosition int
	
	// Menu state
	currentMenu     MenuState
	menuIndex       int
	editMode        bool
	tempValue       int
	customDivider   int // Index into clockDividers
	lastCustomPulse time.Time
	
	// Display buffer
	img    *image.RGBA
	bounds image.Rectangle
	
	// Update control
	updateTicker *time.Ticker
	stopUpdate   chan bool
}

// NewOLEDDisplay creates a new OLED display interface
func NewOLEDDisplay(bridge *EurorackLinkBridge) (*OLEDDisplay, error) {
	// Initialize periph.io
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize periph.io: %v", err)
	}
	
	// Open I2C bus
	bus, err := i2creg.Open("")
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C bus: %v", err)
	}
	
	// Initialize SSD1306 OLED display
	display, err := ssd1306.NewI2C(bus, &ssd1306.Opts{
		W: 128,
		H: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OLED display: %v", err)
	}
	
	// Create display buffer
	bounds := display.Bounds()
	img := image.NewRGBA(bounds)
	
	oled := &OLEDDisplay{
		bridge:        bridge,
		display:       display,
		pins:          defaultEncoderPins,
		encoderLines:  make(map[string]*gpiocdev.Line),
		currentMenu:   MenuMain,
		customDivider: 3, // Default to 4 PPQN
		img:           img,
		bounds:        bounds,
		stopUpdate:    make(chan bool),
	}
	
	// Initialize encoder GPIO
	if err := oled.initEncoder(); err != nil {
		return nil, fmt.Errorf("failed to initialize encoder: %v", err)
	}
	
	// Start update loop
	oled.startUpdateLoop()
	
	return oled, nil
}

// initEncoder sets up rotary encoder and button GPIO
func (o *OLEDDisplay) initEncoder() error {
	// Configure encoder inputs
	encoderConfigs := map[string]int{
		"encoderA":   o.pins.EncoderA,
		"encoderB":   o.pins.EncoderB,
		"encoderBtn": o.pins.EncoderBtn,
		"backBtn":    o.pins.BackBtn,
		"enterBtn":   o.pins.EnterBtn,
	}
	
	for name, pin := range encoderConfigs {
		// Create event handler for this input
		eventHandler := func(inputName string) func(gpiocdev.LineEvent) {
			return func(evt gpiocdev.LineEvent) {
				o.handleEncoderEvent(inputName, evt)
			}
		}(name)
		
		// Request line with appropriate configuration
		var line *gpiocdev.Line
		var err error
		
		if name == "encoderA" || name == "encoderB" {
			// Encoder pins: pull-up with both edge detection
			line, err = gpiocdev.RequestLine("gpiochip0", pin,
				gpiocdev.WithPullUp,
				gpiocdev.WithBothEdges,
				gpiocdev.WithEventHandler(eventHandler))
		} else {
			// Buttons: pull-up with falling edge (button press)
			line, err = gpiocdev.RequestLine("gpiochip0", pin,
				gpiocdev.WithPullUp,
				gpiocdev.WithFallingEdge,
				gpiocdev.WithEventHandler(eventHandler))
		}
		
		if err != nil {
			return fmt.Errorf("failed to configure encoder pin %d (%s): %v", pin, name, err)
		}
		o.encoderLines[name] = line
	}
	
	// Set up custom output
	line, err := gpiocdev.RequestLine("gpiochip0", o.pins.CustomOut, gpiocdev.AsOutput(0))
	if err != nil {
		return fmt.Errorf("failed to configure custom output pin %d: %v", o.pins.CustomOut, err)
	}
	o.encoderLines["customOut"] = line
	
	return nil
}

// handleEncoderEvent processes encoder and button events
func (o *OLEDDisplay) handleEncoderEvent(input string, evt gpiocdev.LineEvent) {
	switch input {
	case "encoderA", "encoderB":
		o.handleRotaryEncoder(input, evt)
	case "encoderBtn":
		if evt.Type == gpiocdev.LineEventFallingEdge {
			o.handleEncoderButton()
		}
	case "backBtn":
		if evt.Type == gpiocdev.LineEventFallingEdge {
			o.handleBackButton()
		}
	case "enterBtn":
		if evt.Type == gpiocdev.LineEventFallingEdge {
			o.handleEnterButton()
		}
	}
}

// handleRotaryEncoder processes rotary encoder rotation
func (o *OLEDDisplay) handleRotaryEncoder(input string, evt gpiocdev.LineEvent) {
	// Read current state of encoder B pin
	lineB := o.encoderLines["encoderB"]
	stateB, _ := lineB.Value()
	
	// Detect rotation direction using quadrature encoding
	if input == "encoderA" && evt.Type == gpiocdev.LineEventFallingEdge {
		if stateB == 1 {
			o.encoderPosition++
		} else {
			o.encoderPosition--
		}
		o.handleEncoderRotation()
	}
}

// handleEncoderRotation processes encoder rotation
func (o *OLEDDisplay) handleEncoderRotation() {
	if o.editMode {
		o.handleEditModeRotation()
	} else {
		o.handleMenuNavigation()
	}
}

// handleMenuNavigation navigates through menus
func (o *OLEDDisplay) handleMenuNavigation() {
	delta := 0
	if o.encoderPosition > 0 {
		delta = 1
		o.encoderPosition = 0
	} else if o.encoderPosition < 0 {
		delta = -1
		o.encoderPosition = 0
	}
	
	if delta == 0 {
		return
	}
	
	switch o.currentMenu {
	case MenuMain:
		o.menuIndex = (o.menuIndex + delta + 5) % 5 // 5 main menu items
	case MenuCustomClock:
		o.menuIndex = (o.menuIndex + delta + len(clockDividers)) % len(clockDividers)
	}
}

// handleEditModeRotation handles value editing
func (o *OLEDDisplay) handleEditModeRotation() {
	delta := 0
	if o.encoderPosition > 0 {
		delta = 1
		o.encoderPosition = 0
	} else if o.encoderPosition < 0 {
		delta = -1
		o.encoderPosition = 0
	}
	
	if delta == 0 {
		return
	}
	
	switch o.currentMenu {
	case MenuTempo:
		o.tempValue += delta
		if o.tempValue < 60 {
			o.tempValue = 60
		}
		if o.tempValue > 200 {
			o.tempValue = 200
		}
	case MenuCustomClock:
		o.customDivider = (o.customDivider + delta + len(clockDividers)) % len(clockDividers)
	}
}

// handleEncoderButton handles encoder button press
func (o *OLEDDisplay) handleEncoderButton() {
	if o.editMode {
		// Save changes and exit edit mode
		switch o.currentMenu {
		case MenuTempo:
			o.bridge.setTempo(float64(o.tempValue))
		case MenuCustomClock:
			// Custom divider is already set by rotation
		}
		o.editMode = false
	} else {
		// Enter edit mode or navigate to submenu
		switch o.currentMenu {
		case MenuMain:
			switch o.menuIndex {
			case 0:
				o.currentMenu = MenuTempo
				o.tempValue = int(o.bridge.lastLinkTempo)
				o.editMode = true
			case 1:
				o.currentMenu = MenuCustomClock
			case 2:
				o.currentMenu = MenuPeers
			case 3:
				o.currentMenu = MenuStatus
			case 4:
				o.currentMenu = MenuSettings
			}
		case MenuCustomClock:
			o.customDivider = o.menuIndex
			o.editMode = true
		}
	}
}

// handleBackButton handles back button press
func (o *OLEDDisplay) handleBackButton() {
	if o.editMode {
		o.editMode = false
	} else {
		o.currentMenu = MenuMain
		o.menuIndex = 0
	}
}

// handleEnterButton handles enter button press (same as encoder button)
func (o *OLEDDisplay) handleEnterButton() {
	o.handleEncoderButton()
}

// setTempo updates the Link tempo
func (b *EurorackLinkBridge) setTempo(tempo float64) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	b.state.SetTempo(tempo, currentTime)
	b.link.CommitAppSessionState(b.state)
	
	b.mu.Lock()
	b.lastLinkTempo = tempo
	b.mu.Unlock()
	
	b.logInfo("Tempo set to %.1f BPM", tempo)
}

// startUpdateLoop begins the display update routine
func (o *OLEDDisplay) startUpdateLoop() {
	o.updateTicker = time.NewTicker(100 * time.Millisecond) // 10 FPS
	
	go func() {
		for {
			select {
			case <-o.updateTicker.C:
				o.updateDisplay()
				o.updateCustomClock()
			case <-o.stopUpdate:
				return
			}
		}
	}()
}

// updateCustomClock generates custom clock pulses
func (o *OLEDDisplay) updateCustomClock() {
	if !o.bridge.state.IsPlaying() {
		return
	}
	
	o.bridge.link.CaptureAppSessionState(o.bridge.state)
	currentTime := o.bridge.link.ClockMicros()
	
	// Calculate current beat
	divider := clockDividers[o.customDivider].Divider
	beat := o.bridge.state.BeatAtTime(currentTime, divider)
	
	// Check if we need to send a pulse
	lastBeat := 0.0
	if !o.lastCustomPulse.IsZero() {
		lastTime := o.lastCustomPulse.UnixMicro()
		lastBeat = o.bridge.state.BeatAtTime(lastTime, divider)
	}
	
	if int(beat) > int(lastBeat) {
		o.sendCustomPulse()
		o.lastCustomPulse = time.Now()
	}
}

// sendCustomPulse sends a pulse to the custom output
func (o *OLEDDisplay) sendCustomPulse() {
	line := o.encoderLines["customOut"]
	if line == nil {
		return
	}
	
	// Send 10ms pulse
	line.SetValue(1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		line.SetValue(0)
	}()
}

// updateDisplay refreshes the OLED display
func (o *OLEDDisplay) updateDisplay() {
	// Clear display
	draw.Draw(o.img, o.bounds, &image.Uniform{}, image.Point{}, draw.Src)
	
	// Draw current menu
	switch o.currentMenu {
	case MenuMain:
		o.drawMainMenu()
	case MenuTempo:
		o.drawTempoMenu()
	case MenuCustomClock:
		o.drawCustomClockMenu()
	case MenuPeers:
		o.drawPeersMenu()
	case MenuStatus:
		o.drawStatusMenu()
	case MenuSettings:
		o.drawSettingsMenu()
	}
	
	// Update display
	o.display.Draw(o.img.Bounds(), o.img, image.Point{})
}

// drawMainMenu draws the main menu
func (o *OLEDDisplay) drawMainMenu() {
	menuItems := []string{
		"Tempo",
		"Custom Clock", 
		"Peers",
		"Status",
		"Settings",
	}
	
	o.drawText(0, 0, "EURORACK LINK", false)
	o.drawText(0, 16, "==============", false)
	
	for i, item := range menuItems {
		selected := i == o.menuIndex
		o.drawText(8, 28+i*8, item, selected)
	}
}

// drawTempoMenu draws the tempo adjustment menu
func (o *OLEDDisplay) drawTempoMenu() {
	o.drawText(0, 0, "TEMPO", false)
	o.drawText(0, 16, "=====", false)
	
	if o.editMode {
		o.drawText(0, 32, fmt.Sprintf("BPM: %d", o.tempValue), true)
		o.drawText(0, 48, "Press to save", false)
	} else {
		tempo := int(o.bridge.lastLinkTempo)
		o.drawText(0, 32, fmt.Sprintf("BPM: %d", tempo), false)
		o.drawText(0, 48, "Press to edit", false)
	}
}

// drawCustomClockMenu draws the custom clock configuration menu
func (o *OLEDDisplay) drawCustomClockMenu() {
	o.drawText(0, 0, "CUSTOM CLOCK", false)
	o.drawText(0, 16, "============", false)
	
	divider := clockDividers[o.customDivider]
	o.drawText(0, 32, fmt.Sprintf("Type: %s", divider.Name), true)
	o.drawText(0, 48, fmt.Sprintf("Div: %.3f", divider.Divider), false)
}

// drawPeersMenu draws the Link peers information
func (o *OLEDDisplay) drawPeersMenu() {
	o.drawText(0, 0, "LINK PEERS", false)
	o.drawText(0, 16, "==========", false)
	
	peers := o.bridge.link.NumPeers()
	o.drawText(0, 32, fmt.Sprintf("Connected: %d", peers), false)
	
	if peers > 0 {
		o.drawText(0, 48, "Network active", false)
	} else {
		o.drawText(0, 48, "No connections", false)
	}
}

// drawStatusMenu draws the system status
func (o *OLEDDisplay) drawStatusMenu() {
	o.drawText(0, 0, "STATUS", false)
	o.drawText(0, 16, "======", false)
	
	o.bridge.link.CaptureAppSessionState(o.bridge.state)
	playing := o.bridge.state.IsPlaying()
	
	if playing {
		o.drawText(0, 28, "Transport: PLAY", false)
	} else {
		o.drawText(0, 28, "Transport: STOP", false)
	}
	
	if o.bridge.externalSyncEnabled {
		o.drawText(0, 40, "Mode: External", false)
	} else {
		o.drawText(0, 40, "Mode: Link", false)
	}
	
	tempo := int(o.bridge.lastLinkTempo)
	o.drawText(0, 52, fmt.Sprintf("BPM: %d", tempo), false)
}

// drawSettingsMenu draws the settings menu
func (o *OLEDDisplay) drawSettingsMenu() {
	o.drawText(0, 0, "SETTINGS", false)
	o.drawText(0, 16, "========", false)
	
	o.drawText(0, 28, "GPIO Config", false)
	o.drawText(0, 36, "Sync Mode", false)
	o.drawText(0, 44, "Reset", false)
	o.drawText(0, 52, "About", false)
}

// drawText draws text at the specified position
func (o *OLEDDisplay) drawText(x, y int, text string, selected bool) {
	// Simple bitmap font rendering - in a real implementation, 
	// you'd use a proper font library like golang.org/x/image/font
	if selected {
		text = "> " + text
	}
	
	// For now, just store the text - in real implementation,
	// render to the image buffer using a bitmap font
	_ = x
	_ = y
	_ = text
}

// Stop shuts down the OLED display
func (o *OLEDDisplay) Stop() {
	if o.updateTicker != nil {
		o.updateTicker.Stop()
	}
	close(o.stopUpdate)
	
	// Clear display
	draw.Draw(o.img, o.bounds, &image.Uniform{}, image.Point{}, draw.Src)
	o.display.Draw(o.img.Bounds(), o.img, image.Point{})
	
	// Close GPIO lines
	for _, line := range o.encoderLines {
		line.Close()
	}
}