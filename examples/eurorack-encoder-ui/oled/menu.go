package oled

import (
	"fmt"
	"image"
	"time"
)

// MenuState represents different menu screens
type MenuState int

const (
	MenuHome MenuState = iota
	MenuTempo
	MenuPeers
	MenuStatus
	MenuPhase
	MenuSwing
	MenuSettings
)

// MenuItem represents a single menu item
type MenuItem struct {
	Title    string
	Value    string
	Action   func()
	State    MenuState
	Icon     string
}

// Menu manages the menu system with smooth animations
type Menu struct {
	display    *Display
	graphics   *Graphics
	
	// Menu state
	currentState    MenuState
	selectedIndex   int
	items          []MenuItem
	
	// Animation state
	isTransitioning bool
	transitionStart time.Time
	transitionDuration time.Duration
	oldBuffer      *image.RGBA
	slideDirection int
	
	// Data callbacks
	onTempoChange   func(float64)
	onPhaseChange   func(string, float64)
	onSwingChange   func(string, float64)
	
	// Live data
	tempo         float64
	isPlaying     bool
	peerCount     int
	beatPhase     float64
	
	// Edit state
	isEditing     bool
	editValue     float64
	editString    string
}

// NewMenu creates a new menu system
func NewMenu(display *Display) *Menu {
	m := &Menu{
		display:            display,
		graphics:           NewGraphics(display),
		currentState:       MenuHome,
		transitionDuration: 300 * time.Millisecond,
	}
	
	m.buildMenuItems()
	return m
}

// SetCallbacks sets the data update callbacks
func (m *Menu) SetCallbacks(onTempo func(float64), onPhase func(string, float64), onSwing func(string, float64)) {
	m.onTempoChange = onTempo
	m.onPhaseChange = onPhase
	m.onSwingChange = onSwing
}

// UpdateData updates the live data shown in menus
func (m *Menu) UpdateData(tempo float64, isPlaying bool, peerCount int, beatPhase float64) {
	m.tempo = tempo
	m.isPlaying = isPlaying
	m.peerCount = peerCount
	m.beatPhase = beatPhase
}

// buildMenuItems creates the menu structure
func (m *Menu) buildMenuItems() {
	switch m.currentState {
	case MenuHome:
		m.items = []MenuItem{
			{"Tempo", "", func() { m.navigateTo(MenuTempo, 1) }, MenuTempo, "♪"},
			{"Peers", "", func() { m.navigateTo(MenuPeers, 1) }, MenuPeers, "⇄"},
			{"Status", "", func() { m.navigateTo(MenuStatus, 1) }, MenuStatus, "●"},
			{"Phase", "", func() { m.navigateTo(MenuPhase, 1) }, MenuPhase, "↻"},
			{"Swing", "", func() { m.navigateTo(MenuSwing, 1) }, MenuSwing, "~"},
			{"Settings", "", func() { m.navigateTo(MenuSettings, 1) }, MenuSettings, "⚙"},
		}
	case MenuTempo:
		if m.isEditing {
			m.items = []MenuItem{
				{"BPM", fmt.Sprintf("%.1f", m.editValue), func() { m.confirmEdit() }, MenuTempo, "♪"},
			}
		} else {
			m.items = []MenuItem{
				{"BPM", fmt.Sprintf("%.1f", m.tempo), func() { m.startEdit() }, MenuTempo, "♪"},
				{"Back", "", func() { m.navigateTo(MenuHome, -1) }, MenuHome, "←"},
			}
		}
	case MenuPeers:
		status := "None"
		if m.peerCount > 0 {
			status = fmt.Sprintf("%d connected", m.peerCount)
		}
		m.items = []MenuItem{
			{"Link Peers", status, nil, MenuPeers, "⇄"},
			{"Back", "", func() { m.navigateTo(MenuHome, -1) }, MenuHome, "←"},
		}
	case MenuStatus:
		playStatus := "Stopped"
		if m.isPlaying {
			playStatus = "Playing"
		}
		m.items = []MenuItem{
			{"Transport", playStatus, nil, MenuStatus, "●"},
			{"Tempo", fmt.Sprintf("%.1f BPM", m.tempo), nil, MenuStatus, "♪"},
			{"Phase", fmt.Sprintf("%.2f", m.beatPhase), nil, MenuStatus, "↻"},
			{"Back", "", func() { m.navigateTo(MenuHome, -1) }, MenuHome, "←"},
		}
	}
}

// HandleInput processes encoder and button input
func (m *Menu) HandleInput(inputType string, value int) {
	if m.isTransitioning {
		return // Ignore input during transitions
	}
	
	switch inputType {
	case "encoder":
		if m.isEditing {
			m.editValue += float64(value)
			if m.editValue < 60 {
				m.editValue = 60
			}
			if m.editValue > 200 {
				m.editValue = 200
			}
		} else {
			m.selectedIndex += value
			if m.selectedIndex < 0 {
				m.selectedIndex = len(m.items) - 1
			}
			if m.selectedIndex >= len(m.items) {
				m.selectedIndex = 0
			}
		}
		
	case "button":
		if m.isEditing {
			m.confirmEdit()
		} else if m.selectedIndex < len(m.items) && m.items[m.selectedIndex].Action != nil {
			m.items[m.selectedIndex].Action()
		}
		
	case "back":
		if m.isEditing {
			m.cancelEdit()
		} else if m.currentState != MenuHome {
			m.navigateTo(MenuHome, -1)
		}
	}
}

// startEdit begins editing the current value
func (m *Menu) startEdit() {
	m.isEditing = true
	m.editValue = m.tempo
	m.buildMenuItems()
}

// confirmEdit saves the edited value
func (m *Menu) confirmEdit() {
	if m.onTempoChange != nil {
		m.onTempoChange(m.editValue)
	}
	m.isEditing = false
	m.buildMenuItems()
}

// cancelEdit cancels the current edit
func (m *Menu) cancelEdit() {
	m.isEditing = false
	m.buildMenuItems()
}

// navigateTo transitions to a new menu state
func (m *Menu) navigateTo(newState MenuState, direction int) {
	if m.isTransitioning {
		return
	}
	
	// Capture current screen
	m.oldBuffer = image.NewRGBA(m.display.Buffer().Bounds())
	copy(m.oldBuffer.Pix, m.display.Buffer().Pix)
	
	// Start transition
	m.isTransitioning = true
	m.transitionStart = time.Now()
	m.slideDirection = direction
	
	// Update state
	m.currentState = newState
	m.selectedIndex = 0
	m.buildMenuItems()
	
	// Start animation
	m.display.StartAnimation(m.transitionDuration, EaseOutQuad)
}

// Render draws the current menu state
func (m *Menu) Render() {
	if m.isTransitioning {
		m.renderTransition()
	} else {
		m.renderMenu()
	}
}

// renderTransition draws the slide transition animation
func (m *Menu) renderTransition() {
	progress := m.display.GetAnimationProgress()
	
	if progress >= 1.0 {
		m.isTransitioning = false
		m.renderMenu()
		return
	}
	
	// Create buffer with new menu content
	newBuffer := image.NewRGBA(m.display.Buffer().Bounds())
	m.display.Clear()
	m.renderMenuToBuffer(newBuffer)
	
	// Draw slide transition
	m.graphics.DrawSlideTransition(progress, m.slideDirection, m.oldBuffer, newBuffer)
}

// renderMenuToBuffer renders menu content to a specific buffer
func (m *Menu) renderMenuToBuffer(buffer *image.RGBA) {
	// Temporarily swap buffers to render to target
	originalBuffer := m.display.buffer
	m.display.buffer = buffer
	
	m.renderMenu()
	
	// Restore original buffer
	m.display.buffer = originalBuffer
}

// renderMenu draws the current menu
func (m *Menu) renderMenu() {
	m.display.Clear()
	
	size := m.display.Size()
	
	// Draw title bar
	if m.currentState == MenuHome {
		m.drawTitleBar("EURORACK LINK")
	} else {
		m.drawTitleBar(m.getMenuTitle())
	}
	
	if m.currentState == MenuHome {
		m.renderHomeMenu()
	} else if size.Height == 32 {
		m.renderCompactMenu()
	} else {
		m.renderFullMenu()
	}
	
	// Add status indicators
	m.drawStatusBar()
}

// getMenuTitle returns the title for the current menu
func (m *Menu) getMenuTitle() string {
	titles := map[MenuState]string{
		MenuTempo:    "TEMPO",
		MenuPeers:    "PEERS", 
		MenuStatus:   "STATUS",
		MenuPhase:    "PHASE",
		MenuSwing:    "SWING",
		MenuSettings: "SETTINGS",
	}
	return titles[m.currentState]
}

// drawTitleBar draws the top title bar
func (m *Menu) drawTitleBar(title string) {
	size := m.display.Size()
	
	// Background
	m.graphics.FillRect(0, 0, size.Width, 12, White)
	
	// Title text (centered)
	textWidth := m.graphics.GetTextWidth(title)
	x := (size.Width - textWidth) / 2
	m.graphics.DrawText(x, 1, title, Black)
	
	// Beat indicator (if playing)
	if m.isPlaying {
		m.graphics.DrawBeatIndicator(size.Width-8, 6, 4, m.beatPhase)
	}
}

// renderHomeMenu draws the main menu with icons
func (m *Menu) renderHomeMenu() {
	size := m.display.Size()
	startY := 15
	
	if size.Height == 32 {
		// Compact 3-item view for 32px height
		m.renderCompactHomeMenu()
		return
	}
	
	// Full menu for 64px height
	for i, item := range m.items {
		y := startY + i*8
		selected := i == m.selectedIndex
		
		if selected {
			// Highlight background
			m.graphics.FillRect(0, y-1, size.Width, 10, White)
			m.graphics.DrawText(2, y, item.Icon+" "+item.Title, Black)
		} else {
			m.graphics.DrawText(2, y, item.Icon+" "+item.Title, White)
		}
	}
	
	// Add scroll indicator if needed
	if len(m.items) > 6 {
		m.graphics.DrawScrollIndicator(size.Width-2, startY, 48, len(m.items), 6, m.selectedIndex)
	}
}

// renderCompactHomeMenu draws home menu for 32px displays
func (m *Menu) renderCompactHomeMenu() {
	size := m.display.Size()
	
	// Show only current and adjacent items
	visibleItems := []MenuItem{}
	visibleIndices := []int{}
	
	// Add previous item
	if m.selectedIndex > 0 {
		visibleItems = append(visibleItems, m.items[m.selectedIndex-1])
		visibleIndices = append(visibleIndices, m.selectedIndex-1)
	}
	
	// Add current item
	visibleItems = append(visibleItems, m.items[m.selectedIndex])
	visibleIndices = append(visibleIndices, m.selectedIndex)
	
	// Add next item
	if m.selectedIndex < len(m.items)-1 {
		visibleItems = append(visibleItems, m.items[m.selectedIndex+1])
		visibleIndices = append(visibleIndices, m.selectedIndex+1)
	}
	
	// Draw items
	y := 14
	for i, item := range visibleItems {
		isSelected := visibleIndices[i] == m.selectedIndex
		
		if isSelected {
			// Highlight current selection
			m.graphics.FillRect(0, y, size.Width, 8, White)
			m.graphics.DrawText(2, y-1, "> "+item.Icon+" "+item.Title, Black)
		} else {
			m.graphics.DrawText(8, y-1, item.Icon+" "+item.Title, White)
		}
		y += 6
	}
	
	// Position indicator
	m.graphics.DrawTextRight(size.Width-2, size.Height-8, 
		fmt.Sprintf("%d/%d", m.selectedIndex+1, len(m.items)), White)
}

// renderCompactMenu draws compact menus for 32px displays
func (m *Menu) renderCompactMenu() {
	size := m.display.Size()
	
	if len(m.items) > 0 {
		item := m.items[m.selectedIndex]
		
		// Main content area
		if item.Value != "" {
			// Show item and value
			m.graphics.DrawText(2, 15, item.Title+":", White)
			m.graphics.DrawTextCentered(22, item.Value, White)
		} else {
			// Just show title
			m.graphics.DrawTextCentered(18, item.Title, White)
		}
		
		// Navigation hints
		if len(m.items) > 1 {
			if m.selectedIndex > 0 {
				m.graphics.DrawText(0, size.Height-8, "↑", Gray)
			}
			if m.selectedIndex < len(m.items)-1 {
				m.graphics.DrawText(0, size.Height-1, "↓", Gray)
			}
		}
	}
}

// renderFullMenu draws full menus for 64px displays
func (m *Menu) renderFullMenu() {
	startY := 15
	
	for i, item := range m.items {
		y := startY + i*12
		selected := i == m.selectedIndex
		
		if selected {
			// Selection background
			m.graphics.FillRect(0, y-2, m.display.Size().Width, 12, White)
			m.graphics.DrawText(4, y, item.Title, Black)
			if item.Value != "" {
				m.graphics.DrawTextRight(m.display.Size().Width-4, y, item.Value, Black)
			}
		} else {
			m.graphics.DrawText(4, y, item.Title, White)
			if item.Value != "" {
				m.graphics.DrawTextRight(m.display.Size().Width-4, y, item.Value, Gray)
			}
		}
	}
}

// drawStatusBar draws the bottom status bar
func (m *Menu) drawStatusBar() {
	size := m.display.Size()
	y := size.Height - 8
	
	// Connection status
	if m.peerCount > 0 {
		m.graphics.DrawText(2, y, fmt.Sprintf("●%d", m.peerCount), White)
	} else {
		m.graphics.DrawText(2, y, "○", Gray)
	}
	
	// Tempo display
	if m.currentState != MenuTempo {
		tempoText := fmt.Sprintf("%.0f", m.tempo)
		m.graphics.DrawTextRight(size.Width-2, y, tempoText, White)
	}
}

// IsAnimating returns true if the menu is currently animating
func (m *Menu) IsAnimating() bool {
	return m.isTransitioning || m.display.IsAnimating()
}