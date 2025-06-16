package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"

	"github.com/warthog618/go-gpiocdev"
	"github.com/waxdred/go-i2c-oled"
	"github.com/waxdred/go-i2c-oled/ssd1306"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
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
	MenuPhaseOffset
	MenuClockSwing
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

// Clock output names for phase offset and swing configuration
var clockOutputs = []string{
	"clock1",  // 1 PPQN
	"clock2",  // 2 PPQN
	"clock4",  // 4 PPQN
	"clock24", // 24 PPQN
}

// OLEDDisplay manages the OLED display and encoder interface
type OLEDDisplay struct {
	bridge    *EurorackLinkBridge
	display   *goi2coled.I2c
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
	
	// Phase offset and swing editing
	selectedOutput    string  // Currently selected output for editing
	tempPhaseOffset   float64 // Temporary phase offset value during editing
	tempSwingAmount   float64 // Temporary swing amount value during editing
	
	// Display buffer
	img    *image.RGBA
	bounds image.Rectangle
	
	// Display dimensions
	displayWidth  int
	displayHeight int
	is32Display   bool // True for 128x32, false for 128x64
	
	// Update control
	updateTicker *time.Ticker
	stopUpdate   chan bool
}

// NewOLEDDisplay creates a new OLED display interface
func NewOLEDDisplay(bridge *EurorackLinkBridge) (*OLEDDisplay, error) {
	bridge.logInfo("Initializing OLED display...")
	
	// Determine display dimensions based on mode
	displayWidth := 128
	displayHeight := 64
	is32Display := false
	
	if bridge.oled32Mode {
		displayHeight = 32
		is32Display = true
		bridge.logInfo("Configuring for 128x32 display")
	} else {
		bridge.logInfo("Configuring for 128x64 display")
	}
	
	// Try different I2C addresses and buses
	busNumbers := []int{1, 0} // Try bus 1 first, then bus 0
	addresses := []int{0x3C, 0x3D}
	var display *goi2coled.I2c
	var err error
	
	for _, bus := range busNumbers {
		for _, addr := range addresses {
			bridge.logInfo("Trying SSD1306 at I2C bus %d, address 0x%02X...", bus, addr)
			
			// Initialize SSD1306 OLED display with go-i2c-oled library
			// NewI2c(vccState, height, width, address, bus)
			display, err = goi2coled.NewI2c(ssd1306.SSD1306_SWITCHCAPVCC, displayHeight, displayWidth, addr, bus)
			if err == nil {
				bridge.logInfo("SSD1306 %dx%d initialized successfully at bus %d, address 0x%02X", displayWidth, displayHeight, bus, addr)
				break
			} else {
				bridge.logInfo("Failed to initialize SSD1306 at bus %d, address 0x%02X: %v", bus, addr, err)
			}
		}
		if display != nil {
			break
		}
	}
	
	if display == nil {
		return nil, fmt.Errorf("failed to initialize OLED display at any bus/address. Last error: %v", err)
	}
	
	// Use the display's built-in image buffer with type assertion
	bounds := display.Img.Bounds()
	img, ok := display.Img.(*image.RGBA)
	if !ok {
		return nil, fmt.Errorf("display image is not RGBA format")
	}
	
	oled := &OLEDDisplay{
		bridge:        bridge,
		display:       display,
		pins:          defaultEncoderPins,
		encoderLines:  make(map[string]*gpiocdev.Line),
		currentMenu:   MenuMain,
		customDivider: 3, // Default to 4 PPQN
		selectedOutput: "clock4", // Default to 4 PPQN output
		img:           img,
		bounds:        bounds,
		displayWidth:  displayWidth,
		displayHeight: displayHeight,
		is32Display:   is32Display,
		stopUpdate:    make(chan bool),
	}
	
	// Initialize encoder GPIO
	if err := oled.initEncoder(); err != nil {
		return nil, fmt.Errorf("failed to initialize encoder: %v", err)
	}
	
	// Test the display with a simple pattern first
	oled.testDisplay()
	
	// Start update loops
	oled.startUpdateLoop()
	oled.startEncoderLoop()
	
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

// handleEncoderEvent processes encoder and button events (fast interrupt handler)
func (o *OLEDDisplay) handleEncoderEvent(input string, evt gpiocdev.LineEvent) {
	// Fast interrupt handler - just queue events
	switch input {
	case "encoderA", "encoderB":
		o.handleRotaryEncoder(input, evt)
	case "encoderBtn":
		if evt.Type == gpiocdev.LineEventFallingEdge {
			select {
			case o.encoderEvents <- EncoderEvent{EventType: "button", Value: 1}:
			default: // Don't block if channel is full
			}
		}
	case "backBtn":
		if evt.Type == gpiocdev.LineEventFallingEdge {
			select {
			case o.encoderEvents <- EncoderEvent{EventType: "back", Value: 1}:
			default:
			}
		}
	case "enterBtn":
		if evt.Type == gpiocdev.LineEventFallingEdge {
			select {
			case o.encoderEvents <- EncoderEvent{EventType: "enter", Value: 1}:
			default:
			}
		}
	}
}

// processEncoderEvent handles encoder events in dedicated thread
func (o *OLEDDisplay) processEncoderEvent(event EncoderEvent) {
	switch event.EventType {
	case "rotation":
		if event.Value > 0 {
			o.encoderPosition++
		} else {
			o.encoderPosition--
		}
		o.handleEncoderRotation()
	case "button":
		o.handleEncoderButton()
	case "back":
		o.handleBackButton()
	case "enter":
		o.handleEnterButton()
	}
}

// handleRotaryEncoder processes rotary encoder rotation (fast interrupt)
func (o *OLEDDisplay) handleRotaryEncoder(input string, evt gpiocdev.LineEvent) {
	// Read current state of encoder B pin
	lineB := o.encoderLines["encoderB"]
	stateB, _ := lineB.Value()
	
	// Detect rotation direction using quadrature encoding
	if input == "encoderA" && evt.Type == gpiocdev.LineEventFallingEdge {
		var direction int
		if stateB == 1 {
			direction = 1
		} else {
			direction = -1
		}
		
		// Queue rotation event for fast processing
		select {
		case o.encoderEvents <- EncoderEvent{EventType: "rotation", Value: direction}:
		default: // Don't block if channel is full
		}
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
		o.menuIndex = (o.menuIndex + delta + 7) % 7 // 7 main menu items
	case MenuCustomClock:
		o.menuIndex = (o.menuIndex + delta + len(clockDividers)) % len(clockDividers)
	case MenuPhaseOffset, MenuClockSwing:
		o.menuIndex = (o.menuIndex + delta + len(clockOutputs)) % len(clockOutputs)
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
	case MenuPhaseOffset:
		o.tempPhaseOffset += float64(delta) * 0.01 // 1% increments
		if o.tempPhaseOffset < 0.0 {
			o.tempPhaseOffset = 0.0
		}
		if o.tempPhaseOffset > 1.0 {
			o.tempPhaseOffset = 1.0
		}
	case MenuClockSwing:
		o.tempSwingAmount += float64(delta) * 0.01 // 1% increments
		if o.tempSwingAmount < 0.0 {
			o.tempSwingAmount = 0.0
		}
		if o.tempSwingAmount > 1.0 {
			o.tempSwingAmount = 1.0
		}
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
		case MenuPhaseOffset:
			o.bridge.setPhaseOffset(o.selectedOutput, o.tempPhaseOffset)
		case MenuClockSwing:
			o.bridge.setSwingAmount(o.selectedOutput, o.tempSwingAmount)
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
				o.currentMenu = MenuPhaseOffset
			case 3:
				o.currentMenu = MenuClockSwing
			case 4:
				o.currentMenu = MenuPeers
			case 5:
				o.currentMenu = MenuStatus
			case 6:
				o.currentMenu = MenuSettings
			}
		case MenuCustomClock:
			o.customDivider = o.menuIndex
			o.editMode = true
		case MenuPhaseOffset:
			o.selectedOutput = clockOutputs[o.menuIndex]
			o.tempPhaseOffset = o.bridge.getPhaseOffset(o.selectedOutput)
			o.editMode = true
		case MenuClockSwing:
			o.selectedOutput = clockOutputs[o.menuIndex]
			o.tempSwingAmount = o.bridge.getSwingAmount(o.selectedOutput)
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

// setPhaseOffset sets the phase offset for a specific output
func (b *EurorackLinkBridge) setPhaseOffset(output string, offset float64) {
	b.mu.Lock()
	b.phaseOffsets[output] = offset
	b.mu.Unlock()
	
	b.logInfo("Phase offset for %s set to %.1f° (%.3f)", output, offset*360, offset)
	b.saveConfig() // Auto-save on change
}

// getPhaseOffset gets the phase offset for a specific output
func (b *EurorackLinkBridge) getPhaseOffset(output string) float64 {
	b.mu.RLock()
	offset, exists := b.phaseOffsets[output]
	b.mu.RUnlock()
	
	if !exists {
		return 0.0 // Default to no offset
	}
	return offset
}

// setSwingAmount sets the swing amount for a specific output
func (b *EurorackLinkBridge) setSwingAmount(output string, swing float64) {
	b.mu.Lock()
	b.swingAmounts[output] = swing
	b.mu.Unlock()
	
	b.logInfo("Swing amount for %s set to %.1f%% (%.3f)", output, swing*100, swing)
	b.saveConfig() // Auto-save on change
}

// getSwingAmount gets the swing amount for a specific output
func (b *EurorackLinkBridge) getSwingAmount(output string) float64 {
	b.mu.RLock()
	swing, exists := b.swingAmounts[output]
	b.mu.RUnlock()
	
	if !exists {
		return 0.0 // Default to no swing
	}
	return swing
}

// startUpdateLoop begins the display update routine
func (o *OLEDDisplay) startUpdateLoop() {
	o.updateTicker = time.NewTicker(30 * time.Millisecond) // 33 FPS for smooth UI
	
	go func() {
		for {
			select {
			case <-o.updateTicker.C:
				// Only update display if something changed
				o.updateMutex.RLock()
				needsUpdate := o.needsUpdate
				o.updateMutex.RUnlock()
				
				if needsUpdate {
					o.updateDisplay()
					o.updateMutex.Lock()
					o.needsUpdate = false
					o.updateMutex.Unlock()
				}
				o.updateCustomClock()
			case <-o.stopUpdate:
				return
			}
		}
	}()
}

// startEncoderLoop begins the fast encoder processing routine
func (o *OLEDDisplay) startEncoderLoop() {
	go func() {
		for {
			select {
			case event := <-o.encoderEvents:
				o.processEncoderEvent(event)
				// Mark display for update
				o.updateMutex.Lock()
				o.needsUpdate = true
				o.updateMutex.Unlock()
			case <-o.stopEncoder:
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
	// Clear display buffer (fill with black)
	draw.Draw(o.img, o.img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
	
	// Draw current menu
	switch o.currentMenu {
	case MenuMain:
		o.drawMainMenu()
	case MenuTempo:
		o.drawTempoMenu()
	case MenuCustomClock:
		o.drawCustomClockMenu()
	case MenuPhaseOffset:
		o.drawPhaseOffsetMenu()
	case MenuClockSwing:
		o.drawClockSwingMenu()
	case MenuPeers:
		o.drawPeersMenu()
	case MenuStatus:
		o.drawStatusMenu()
	case MenuSettings:
		o.drawSettingsMenu()
	}
	
	// Update display - send buffer to hardware using go-i2c-oled API
	o.display.Draw()
	err := o.display.Display()
	if err != nil {
		o.bridge.logInfo("Display update error: %v", err)
	}
}

// drawMainMenu draws the main menu
func (o *OLEDDisplay) drawMainMenu() {
	menuItems := []string{
		"Tempo",
		"Custom Clock",
		"Phase Offset",
		"Clock Swing", 
		"Peers",
		"Status",
		"Settings",
	}
	
	if o.is32Display {
		o.drawMainMenu32(menuItems)
	} else {
		o.drawMainMenu64(menuItems)
	}
}

// drawMainMenu64 draws the main menu for 128x64 displays
func (o *OLEDDisplay) drawMainMenu64(menuItems []string) {
	o.drawText(0, 0, "EURORACK LINK", false)
	o.drawText(0, 16, "==============", false)
	
	for i, item := range menuItems {
		selected := i == o.menuIndex
		o.drawText(8, 28+i*8, item, selected)
	}
}

// drawMainMenu32 draws the main menu for 128x32 displays (compact layout)
func (o *OLEDDisplay) drawMainMenu32(menuItems []string) {
	// Show only current selection and adjacent items for 32px height
	currentItem := menuItems[o.menuIndex]
	
	// Title line
	o.drawText(0, 0, "EURORACK", false)
	
	// Show previous item (if exists)
	if o.menuIndex > 0 {
		prevItem := menuItems[o.menuIndex-1]
		o.drawText(8, 8, prevItem, false)
	}
	
	// Show current item (highlighted)
	o.drawText(0, 16, "> " + currentItem, true)
	
	// Show next item (if exists)
	if o.menuIndex < len(menuItems)-1 {
		nextItem := menuItems[o.menuIndex+1]
		o.drawText(8, 24, nextItem, false)
	}
}

// drawTempoMenu draws the tempo adjustment menu
func (o *OLEDDisplay) drawTempoMenu() {
	if o.is32Display {
		o.drawTempoMenu32()
	} else {
		o.drawTempoMenu64()
	}
}

// drawTempoMenu64 draws the tempo menu for 128x64 displays
func (o *OLEDDisplay) drawTempoMenu64() {
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

// drawTempoMenu32 draws the tempo menu for 128x32 displays
func (o *OLEDDisplay) drawTempoMenu32() {
	o.drawText(0, 0, "TEMPO", false)
	
	if o.editMode {
		o.drawText(0, 8, fmt.Sprintf("BPM: %d", o.tempValue), true)
		o.drawText(0, 16, "Rotate: adjust", false)
		o.drawText(0, 24, "Press: save", false)
	} else {
		tempo := int(o.bridge.lastLinkTempo)
		o.drawText(0, 8, fmt.Sprintf("BPM: %d", tempo), false)
		o.drawText(0, 16, "Press: edit", false)
		o.drawText(0, 24, "Back: menu", false)
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

// drawPhaseOffsetMenu draws the phase offset configuration menu
func (o *OLEDDisplay) drawPhaseOffsetMenu() {
	if o.is32Display {
		o.drawPhaseOffsetMenu32()
	} else {
		o.drawPhaseOffsetMenu64()
	}
}

// drawPhaseOffsetMenu64 draws the phase offset menu for 128x64 displays
func (o *OLEDDisplay) drawPhaseOffsetMenu64() {
	o.drawText(0, 0, "PHASE OFFSET", false)
	o.drawText(0, 16, "============", false)
	
	if o.editMode {
		output := o.selectedOutput
		degrees := int(o.tempPhaseOffset * 360)
		o.drawText(0, 28, fmt.Sprintf("Output: %s", output), false)
		o.drawText(0, 40, fmt.Sprintf("Phase: %d°", degrees), true)
		o.drawText(0, 52, "Press to save", false)
	} else {
		for i, output := range clockOutputs {
			selected := i == o.menuIndex
			offset := o.bridge.getPhaseOffset(output)
			degrees := int(offset * 360)
			text := fmt.Sprintf("%s: %d°", output, degrees)
			o.drawText(8, 28+i*8, text, selected)
		}
	}
}

// drawPhaseOffsetMenu32 draws the phase offset menu for 128x32 displays
func (o *OLEDDisplay) drawPhaseOffsetMenu32() {
	o.drawText(0, 0, "PHASE", false)
	
	if o.editMode {
		output := o.selectedOutput
		degrees := int(o.tempPhaseOffset * 360)
		o.drawText(0, 8, fmt.Sprintf("%s: %d°", output, degrees), true)
		o.drawText(0, 16, "Rotate: adjust", false)
		o.drawText(0, 24, "Press: save", false)
	} else {
		// Show current selection with navigation context
		output := clockOutputs[o.menuIndex]
		offset := o.bridge.getPhaseOffset(output)
		degrees := int(offset * 360)
		
		o.drawText(0, 8, fmt.Sprintf("> %s", output), true)
		o.drawText(0, 16, fmt.Sprintf("  %d°", degrees), false)
		o.drawText(0, 24, "Press: edit", false)
	}
}

// drawClockSwingMenu draws the clock swing configuration menu
func (o *OLEDDisplay) drawClockSwingMenu() {
	if o.is32Display {
		o.drawClockSwingMenu32()
	} else {
		o.drawClockSwingMenu64()
	}
}

// drawClockSwingMenu64 draws the clock swing menu for 128x64 displays
func (o *OLEDDisplay) drawClockSwingMenu64() {
	o.drawText(0, 0, "CLOCK SWING", false)
	o.drawText(0, 16, "===========", false)
	
	if o.editMode {
		output := o.selectedOutput
		percent := int(o.tempSwingAmount * 100)
		o.drawText(0, 28, fmt.Sprintf("Output: %s", output), false)
		o.drawText(0, 40, fmt.Sprintf("Swing: %d%%", percent), true)
		o.drawText(0, 52, "Press to save", false)
	} else {
		for i, output := range clockOutputs {
			selected := i == o.menuIndex
			swing := o.bridge.getSwingAmount(output)
			percent := int(swing * 100)
			text := fmt.Sprintf("%s: %d%%", output, percent)
			o.drawText(8, 28+i*8, text, selected)
		}
	}
}

// drawClockSwingMenu32 draws the clock swing menu for 128x32 displays
func (o *OLEDDisplay) drawClockSwingMenu32() {
	o.drawText(0, 0, "SWING", false)
	
	if o.editMode {
		output := o.selectedOutput
		percent := int(o.tempSwingAmount * 100)
		o.drawText(0, 8, fmt.Sprintf("%s: %d%%", output, percent), true)
		o.drawText(0, 16, "Rotate: adjust", false)
		o.drawText(0, 24, "Press: save", false)
	} else {
		// Show current selection with navigation context
		output := clockOutputs[o.menuIndex]
		swing := o.bridge.getSwingAmount(output)
		percent := int(swing * 100)
		
		o.drawText(0, 8, fmt.Sprintf("> %s", output), true)
		o.drawText(0, 16, fmt.Sprintf("  %d%%", percent), false)
		o.drawText(0, 24, "Press: edit", false)
	}
}

// drawPeersMenu draws the Link peers information
func (o *OLEDDisplay) drawPeersMenu() {
	if o.is32Display {
		o.drawPeersMenu32()
	} else {
		o.drawPeersMenu64()
	}
}

// drawPeersMenu64 draws the peers menu for 128x64 displays
func (o *OLEDDisplay) drawPeersMenu64() {
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

// drawPeersMenu32 draws the peers menu for 128x32 displays
func (o *OLEDDisplay) drawPeersMenu32() {
	o.drawText(0, 0, "PEERS", false)
	
	peers := o.bridge.link.NumPeers()
	o.drawText(0, 8, fmt.Sprintf("Count: %d", peers), false)
	
	if peers > 0 {
		o.drawText(0, 16, "Status: Active", false)
	} else {
		o.drawText(0, 16, "Status: None", false)
	}
	o.drawText(0, 24, "Back: menu", false)
}

// drawStatusMenu draws the system status
func (o *OLEDDisplay) drawStatusMenu() {
	if o.is32Display {
		o.drawStatusMenu32()
	} else {
		o.drawStatusMenu64()
	}
}

// drawStatusMenu64 draws the status menu for 128x64 displays
func (o *OLEDDisplay) drawStatusMenu64() {
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

// drawStatusMenu32 draws the status menu for 128x32 displays
func (o *OLEDDisplay) drawStatusMenu32() {
	o.drawText(0, 0, "STATUS", false)
	
	o.bridge.link.CaptureAppSessionState(o.bridge.state)
	playing := o.bridge.state.IsPlaying()
	tempo := int(o.bridge.lastLinkTempo)
	
	// Show most important info in compact form
	if playing {
		o.drawText(0, 8, "PLAY", true)
	} else {
		o.drawText(0, 8, "STOP", false)
	}
	
	o.drawText(0, 16, fmt.Sprintf("BPM: %d", tempo), false)
	
	if o.bridge.externalSyncEnabled {
		o.drawText(0, 24, "Ext Sync", false)
	} else {
		o.drawText(0, 24, "Link Mode", false)
	}
}

// drawSettingsMenu draws the settings menu
func (o *OLEDDisplay) drawSettingsMenu() {
	if o.is32Display {
		o.drawSettingsMenu32()
	} else {
		o.drawSettingsMenu64()
	}
}

// drawSettingsMenu64 draws the settings menu for 128x64 displays
func (o *OLEDDisplay) drawSettingsMenu64() {
	o.drawText(0, 0, "SETTINGS", false)
	o.drawText(0, 16, "========", false)
	
	o.drawText(0, 28, "GPIO Config", false)
	o.drawText(0, 36, "Sync Mode", false)
	o.drawText(0, 44, "Reset", false)
	o.drawText(0, 52, "About", false)
}

// drawSettingsMenu32 draws the settings menu for 128x32 displays
func (o *OLEDDisplay) drawSettingsMenu32() {
	o.drawText(0, 0, "SETTINGS", false)
	o.drawText(0, 8, "GPIO Config", false)
	o.drawText(0, 16, "Sync Mode", false)
	o.drawText(0, 24, "Back: menu", false)
}

// testDisplay draws a simple test pattern to verify the display is working
func (o *OLEDDisplay) testDisplay() {
	o.bridge.logInfo("Testing display with pattern...")
	
	// Simple border test
	white := color.RGBA{255, 255, 255, 255}
	
	// Draw border
	for x := 0; x < o.displayWidth; x++ {
		o.img.Set(x, 0, white)                       // Top
		o.img.Set(x, o.displayHeight-1, white)      // Bottom
	}
	for y := 0; y < o.displayHeight; y++ {
		o.img.Set(0, y, white)                       // Left
		o.img.Set(o.displayWidth-1, y, white)       // Right
	}
	
	// Draw center cross
	centerX := o.displayWidth / 2
	centerY := o.displayHeight / 2
	for i := -4; i <= 4; i++ {
		if centerX+i >= 0 && centerX+i < o.displayWidth {
			o.img.Set(centerX+i, centerY, white)
		}
		if centerY+i >= 0 && centerY+i < o.displayHeight {
			o.img.Set(centerX, centerY+i, white)
		}
	}
	
	// Update display with test pattern
	o.display.Draw()
	err := o.display.Display()
	if err != nil {
		o.bridge.logInfo("Test pattern display error: %v", err)
	}
	
	// Wait 1 second to show the pattern
	time.Sleep(1 * time.Second)
	
	o.bridge.logInfo("Test pattern complete, starting normal operation...")
}

// Simple UI drawing functions using basic shapes and patterns
func (o *OLEDDisplay) fillRect(x, y, width, height int, color color.RGBA) {
	for py := y; py < y+height && py < o.displayHeight; py++ {
		for px := x; px < x+width && px < o.displayWidth; px++ {
			if px >= 0 && py >= 0 {
				o.img.Set(px, py, color)
			}
		}
	}
}

func (o *OLEDDisplay) drawHLine(x, y, width int, color color.RGBA) {
	for px := x; px < x+width && px < o.displayWidth; px++ {
		if px >= 0 && y >= 0 && y < o.displayHeight {
			o.img.Set(px, y, color)
		}
	}
}

func (o *OLEDDisplay) drawVLine(x, y, height int, color color.RGBA) {
	for py := y; py < y+height && py < o.displayHeight; py++ {
		if x >= 0 && py >= 0 && x < o.displayWidth {
			o.img.Set(x, py, color)
		}
	}
}

// Professional text rendering using proper bitmap fonts
func (o *OLEDDisplay) drawText(x, y int, text string, selected bool) {
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	
	// Choose appropriate font based on display size
	var face font.Face
	var lineHeight int
	
	if o.is32Display {
		// Use smaller font for 32px displays
		face = basicfont.Face7x13
		lineHeight = 13
	} else {
		// Use larger font for 64px displays  
		face = basicfont.Face7x13
		lineHeight = 13
	}
	
	// Calculate text dimensions
	textBounds, _ := font.BoundString(face, text)
	textWidth := int(textBounds.Max.X-textBounds.Min.X) >> 6  // Convert from fixed.Int26_6
	
	if selected {
		// Draw selection background
		bgPadding := 2
		if o.is32Display {
			// Full width selection for 32px displays
			o.fillRect(0, y-bgPadding, o.displayWidth, lineHeight+bgPadding, white)
		} else {
			// Fitted selection for 64px displays
			o.fillRect(x-bgPadding, y-bgPadding, textWidth+2*bgPadding, lineHeight+bgPadding, white)
		}
		
		// Draw text in black on white background
		o.drawStringWithFont(x, y+lineHeight-2, text, face, black)
	} else {
		// Normal white text on black background
		o.drawStringWithFont(x, y+lineHeight-2, text, face, white)
	}
}

// Draw string using the specified font face
func (o *OLEDDisplay) drawStringWithFont(x, y int, text string, face font.Face, textColor color.RGBA) {
	drawer := &font.Drawer{
		Dst:  o.img,
		Src:  &image.Uniform{textColor},
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	drawer.DrawString(text)
}


// Stop shuts down the OLED display
func (o *OLEDDisplay) Stop() {
	if o.updateTicker != nil {
		o.updateTicker.Stop()
	}
	
	// Stop both update loops
	close(o.stopUpdate)
	close(o.stopEncoder)
	
	// Clear display buffer and display
	draw.Draw(o.img, o.img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
	o.display.Draw()
	o.display.Display()
	
	// Close GPIO lines
	for _, line := range o.encoderLines {
		line.Close()
	}
}

