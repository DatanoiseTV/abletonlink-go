package oled

import (
	"sync"
	"time"

	"github.com/warthog618/go-gpiocdev"
)

// InputEvent represents an input event
type InputEvent struct {
	Type      string  // "encoder", "button", "back"
	Value     int     // encoder: direction (-1/+1), buttons: 1 for press
	Timestamp time.Time
}

// InputHandler manages encoder and button input with professional debouncing
type InputHandler struct {
	// GPIO configuration
	encoderA    int
	encoderB    int
	buttonPin   int
	backPin     int
	
	// GPIO lines
	lines map[string]*gpiocdev.Line
	
	// Event processing
	eventChan   chan InputEvent
	stopChan    chan bool
	
	// Encoder state
	lastEncoderState int
	encoderMutex     sync.RWMutex
	lastEncoderTime  time.Time
	
	// Button debouncing
	buttonStates     map[string]*ButtonState
	buttonMutex      sync.RWMutex
}

// ButtonState tracks button press state and timing
type ButtonState struct {
	lastPress      time.Time
	lastRelease    time.Time
	isPressed      bool
	longPressed    bool
	pressDuration  time.Duration
}

const (
	// Timing constants
	encoderDebounceTime = 2 * time.Millisecond
	buttonDebounceTime  = 50 * time.Millisecond
	longPressTime      = 800 * time.Millisecond
	
	// Event buffer size
	eventBufferSize = 20
)

// NewInputHandler creates a new input handler
func NewInputHandler(encoderA, encoderB, buttonPin, backPin int) *InputHandler {
	return &InputHandler{
		encoderA:     encoderA,
		encoderB:     encoderB,
		buttonPin:    buttonPin,
		backPin:      backPin,
		lines:        make(map[string]*gpiocdev.Line),
		eventChan:    make(chan InputEvent, eventBufferSize),
		stopChan:     make(chan bool),
		buttonStates: make(map[string]*ButtonState),
	}
}

// Initialize sets up GPIO lines and starts event processing
func (h *InputHandler) Initialize() error {
	// Initialize button states
	h.buttonStates["button"] = &ButtonState{}
	h.buttonStates["back"] = &ButtonState{}
	
	// Configure encoder pins
	if err := h.setupEncoderPin("encoderA", h.encoderA); err != nil {
		return err
	}
	if err := h.setupEncoderPin("encoderB", h.encoderB); err != nil {
		return err
	}
	
	// Configure button pins
	if err := h.setupButtonPin("button", h.buttonPin); err != nil {
		return err
	}
	if err := h.setupButtonPin("back", h.backPin); err != nil {
		return err
	}
	
	return nil
}

// setupEncoderPin configures an encoder GPIO pin
func (h *InputHandler) setupEncoderPin(name string, pin int) error {
	eventHandler := func(evt gpiocdev.LineEvent) {
		h.handleEncoderEvent(name, evt)
	}
	
	line, err := gpiocdev.RequestLine("gpiochip0", pin,
		gpiocdev.WithPullUp,
		gpiocdev.WithBothEdges,
		gpiocdev.WithEventHandler(eventHandler))
	
	if err != nil {
		return err
	}
	
	h.lines[name] = line
	return nil
}

// setupButtonPin configures a button GPIO pin
func (h *InputHandler) setupButtonPin(name string, pin int) error {
	eventHandler := func(evt gpiocdev.LineEvent) {
		h.handleButtonEvent(name, evt)
	}
	
	line, err := gpiocdev.RequestLine("gpiochip0", pin,
		gpiocdev.WithPullUp,
		gpiocdev.WithBothEdges,  // Both edges for press/release detection
		gpiocdev.WithEventHandler(eventHandler))
	
	if err != nil {
		return err
	}
	
	h.lines[name] = line
	return nil
}

// handleEncoderEvent processes encoder rotation with improved quadrature decoding
func (h *InputHandler) handleEncoderEvent(pin string, evt gpiocdev.LineEvent) {
	now := time.Now()
	
	// Debounce encoder
	h.encoderMutex.Lock()
	if now.Sub(h.lastEncoderTime) < encoderDebounceTime {
		h.encoderMutex.Unlock()
		return
	}
	h.lastEncoderTime = now
	h.encoderMutex.Unlock()
	
	// Read both encoder pins
	lineA := h.lines["encoderA"]
	lineB := h.lines["encoderB"]
	
	stateA, _ := lineA.Value()
	stateB, _ := lineB.Value()
	
	// Combine into 2-bit state
	currentState := (stateA << 1) | stateB
	
	h.encoderMutex.Lock()
	lastState := h.lastEncoderState
	h.lastEncoderState = currentState
	h.encoderMutex.Unlock()
	
	// Full detent quadrature decoding (only trigger on complete detents)
	var direction int
	switch (lastState << 2) | currentState {
	case 0x08: // 10 -> 00 (complete clockwise detent)
		direction = 1
	case 0x04: // 01 -> 00 (complete counter-clockwise detent)
		direction = -1
	default:
		return // Intermediate state or no change
	}
	
	// Send encoder event
	select {
	case h.eventChan <- InputEvent{
		Type:      "encoder",
		Value:     direction,
		Timestamp: now,
	}:
	default:
		// Channel full, drop event
	}
}

// handleButtonEvent processes button press/release with debouncing and long press
func (h *InputHandler) handleButtonEvent(buttonName string, evt gpiocdev.LineEvent) {
	now := time.Now()
	
	h.buttonMutex.Lock()
	state := h.buttonStates[buttonName]
	h.buttonMutex.Unlock()
	
	if evt.Type == gpiocdev.LineEventFallingEdge {
		// Button pressed
		if now.Sub(state.lastPress) < buttonDebounceTime {
			return // Debounce
		}
		
		state.lastPress = now
		state.isPressed = true
		state.longPressed = false
		
		// Start long press detection
		if buttonName == "button" {
			go h.detectLongPress(buttonName, now)
		}
		
	} else if evt.Type == gpiocdev.LineEventRisingEdge {
		// Button released
		if !state.isPressed || now.Sub(state.lastRelease) < buttonDebounceTime {
			return // Debounce or wasn't pressed
		}
		
		state.lastRelease = now
		state.isPressed = false
		pressDuration := now.Sub(state.lastPress)
		state.pressDuration = pressDuration
		
		// Determine event type based on press duration and button
		if buttonName == "button" {
			if pressDuration >= longPressTime || state.longPressed {
				// Long press = back action
				select {
				case h.eventChan <- InputEvent{
					Type:      "back",
					Value:     1,
					Timestamp: now,
				}:
				default:
				}
			} else {
				// Short press = button action
				select {
				case h.eventChan <- InputEvent{
					Type:      "button", 
					Value:     1,
					Timestamp: now,
				}:
				default:
				}
			}
		} else {
			// Other buttons send their own events
			select {
			case h.eventChan <- InputEvent{
				Type:      buttonName,
				Value:     1,
				Timestamp: now,
			}:
			default:
			}
		}
	}
}

// detectLongPress runs in a goroutine to detect long button presses
func (h *InputHandler) detectLongPress(buttonName string, pressTime time.Time) {
	time.Sleep(longPressTime)
	
	h.buttonMutex.Lock()
	state := h.buttonStates[buttonName]
	
	// Check if button is still pressed and this is the same press event
	if state.isPressed && pressTime.Equal(state.lastPress) {
		state.longPressed = true
		
		// Send long press event immediately
		select {
		case h.eventChan <- InputEvent{
			Type:      "back",
			Value:     1,
			Timestamp: time.Now(),
		}:
		default:
		}
	}
	h.buttonMutex.Unlock()
}

// GetEvents returns the event channel for reading input events
func (h *InputHandler) GetEvents() <-chan InputEvent {
	return h.eventChan
}

// Close shuts down the input handler
func (h *InputHandler) Close() {
	close(h.stopChan)
	
	// Close all GPIO lines
	for _, line := range h.lines {
		line.Close()
	}
}