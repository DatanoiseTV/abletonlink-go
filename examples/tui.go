package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TUIManager handles the modern terminal user interface
type TUIManager struct {
	app            *tview.Application
	pages          *tview.Pages
	bridge         *MIDILinkBridge
	
	// Main layout components
	headerBar      *tview.TextView
	statusPanel    *tview.Table
	beatPanel      *tview.TextView
	tempoPanel     *tview.TextView
	syncPanel      *tview.TextView
	logPanel       *tview.TextView
	footerBar      *tview.TextView
	
	// Modal components
	helpModal      *tview.Modal
	
	// Update control
	updateTicker   *time.Ticker
	stopUpdate     chan bool
}

// NewTUIManager creates a new modern TUI manager
func NewTUIManager(bridge *MIDILinkBridge) *TUIManager {
	tui := &TUIManager{
		app:        tview.NewApplication(),
		pages:      tview.NewPages(),
		bridge:     bridge,
		stopUpdate: make(chan bool),
	}
	
	tui.setupComponents()
	tui.setupLayout()
	tui.setupKeyBindings()
	tui.startUpdateLoop()
	
	return tui
}

// setupComponents creates all the UI components with modern styling
func (tui *TUIManager) setupComponents() {
	// Header bar with title and status
	tui.headerBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[white:blue:b] MIDI-Link Bridge v2.0 [::-] [yellow]● RUNNING")
	
	// Status information panel
	tui.statusPanel = tview.NewTable().
		SetBorders(false).
		SetSelectable(false, false)
	tui.statusPanel.SetTitle(" System Status ").SetBorder(true)
	
	// Beat visualization panel with large display
	tui.beatPanel = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetWrap(false)
	tui.beatPanel.SetTitle(" Beat & Bar ").SetBorder(true)
	
	// Tempo and timing panel
	tui.tempoPanel = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetWrap(false)
	tui.tempoPanel.SetTitle(" Tempo & Timing ").SetBorder(true)
	
	// Sync status panel
	tui.syncPanel = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true)
	tui.syncPanel.SetTitle(" Sync Status ").SetBorder(true)
	
	// Log panel with scrolling
	tui.logPanel = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetChangedFunc(func() {
			tui.logPanel.ScrollToEnd()
			tui.app.Draw()
		})
	tui.logPanel.SetTitle(" Log Messages ").SetBorder(true)
	
	// Footer with key bindings
	tui.footerBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[black:white] ↑/↓ BPM [black:white] +/- Phase [black:white] Space Transport [black:white] S Sync [black:white] H Help [black:white] Q Quit ")
	
	// Help modal
	tui.helpModal = tview.NewModal().
		SetText("MIDI-Link Bridge Controls\n\n" +
			"↑/↓: Adjust BPM (±1.0)\n" +
			"+/-: Adjust phase offset (±0.5ms)\n" +
			"Space: Toggle transport start/stop\n" +
			"S: Toggle Link start/stop sync\n" +
			"R: Reset manual phase offset\n" +
			"H: Show/hide this help\n" +
			"Q or Esc: Quit application\n\n" +
			"Phase offset range: ±30ms\n" +
			"Settings are automatically saved").
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			tui.pages.HidePage("help")
		})
}

// setupLayout creates the main application layout
func (tui *TUIManager) setupLayout() {
	// Top row: Status and Beat panels
	topRow := tview.NewFlex().
		AddItem(tui.statusPanel, 0, 1, false).
		AddItem(tui.beatPanel, 0, 1, false).
		AddItem(tui.tempoPanel, 0, 1, false)
	
	// Middle row: Sync panel
	middleRow := tui.syncPanel
	
	// Bottom row: Log panel  
	bottomRow := tui.logPanel
	
	// Main content area
	mainContent := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(topRow, 12, 0, false).        // Fixed height for top panels
		AddItem(middleRow, 8, 0, false).      // Fixed height for sync status
		AddItem(bottomRow, 0, 1, false)       // Remaining space for logs
	
	// Complete layout with header and footer
	fullLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tui.headerBar, 1, 0, false).   // Header bar
		AddItem(mainContent, 0, 1, true).      // Main content
		AddItem(tui.footerBar, 1, 0, false)    // Footer bar
	
	// Add main page
	tui.pages.AddPage("main", fullLayout, true, true)
	
	// Add help modal
	tui.pages.AddPage("help", tui.helpModal, true, false)
	
	// Set root
	tui.app.SetRoot(tui.pages, true)
}

// setupKeyBindings configures global key handlers
func (tui *TUIManager) setupKeyBindings() {
	tui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Rune() == 'q' || event.Rune() == 'Q' || event.Key() == tcell.KeyEscape:
			// Check if help modal is visible
			name, _ := tui.pages.GetFrontPage()
			if name == "help" {
				tui.pages.HidePage("help")
				return nil
			}
			tui.Stop()
			return nil
			
		case event.Rune() == 'h' || event.Rune() == 'H':
			if tui.pages.HasPage("help") {
				// Check if help is currently visible by trying to get the front page
				name, _ := tui.pages.GetFrontPage()
				if name == "help" {
					tui.pages.HidePage("help")
				} else {
					tui.pages.ShowPage("help")
				}
			} else {
				tui.pages.ShowPage("help")
			}
			return nil
			
		case event.Key() == tcell.KeyUp:
			tui.bridge.adjustTempo(1.0)
			return nil
			
		case event.Key() == tcell.KeyDown:
			tui.bridge.adjustTempo(-1.0)
			return nil
			
		case event.Rune() == '+' || event.Rune() == '=':
			tui.bridge.adjustManualPhaseOffset(500 * time.Microsecond)
			return nil
			
		case event.Rune() == '-':
			tui.bridge.adjustManualPhaseOffset(-500 * time.Microsecond)
			return nil
			
		case event.Rune() == ' ':
			tui.bridge.toggleTransport()
			return nil
			
		case event.Rune() == 's' || event.Rune() == 'S':
			tui.bridge.toggleStartStopSync()
			return nil
			
		case event.Rune() == 'r' || event.Rune() == 'R':
			tui.bridge.resetManualPhaseOffset()
			return nil
		}
		
		return event
	})
}

// startUpdateLoop begins the UI update routine
func (tui *TUIManager) startUpdateLoop() {
	tui.updateTicker = time.NewTicker(50 * time.Millisecond) // 20 FPS
	
	go func() {
		for {
			select {
			case <-tui.updateTicker.C:
				tui.app.QueueUpdateDraw(func() {
					tui.updateAllPanels()
				})
			case <-tui.stopUpdate:
				return
			}
		}
	}()
}

// updateAllPanels refreshes all UI components
func (tui *TUIManager) updateAllPanels() {
	tui.updateStatusPanel()
	tui.updateBeatPanel()
	tui.updateTempoPanel()
	tui.updateSyncPanel()
}

// updateStatusPanel refreshes the system status information
func (tui *TUIManager) updateStatusPanel() {
	tui.bridge.mu.RLock()
	linkPlaying := tui.bridge.linkIsPlaying
	midiPlaying := tui.bridge.midiIsPlaying
	externalSync := tui.bridge.externalSyncEnabled
	clockCount := tui.bridge.midiClockCount
	startStopSync := tui.bridge.startStopSyncEnabled
	tui.bridge.mu.RUnlock()
	
	// Clear and rebuild table
	tui.statusPanel.Clear()
	
	row := 0
	
	// Mode
	mode := "[green]Link Master"
	if externalSync {
		mode = "[yellow]MIDI Master"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Mode:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(mode))
	row++
	
	// Transport states
	linkStatus := "[red]Stopped"
	if linkPlaying {
		linkStatus = "[green]Playing"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Link:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(linkStatus))
	row++
	
	midiStatus := "[red]Stopped"
	if midiPlaying {
		midiStatus = "[green]Playing"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("MIDI:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(midiStatus))
	row++
	
	// Start/Stop Sync
	syncStatus := "[red]Disabled"
	if startStopSync {
		syncStatus = "[green]Enabled"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Sync:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(syncStatus))
	row++
	
	// Clock count
	if clockCount > 0 {
		tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Clocks:"))
		tui.statusPanel.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("[cyan]%d", clockCount)))
		row++
	}
}

// updateBeatPanel creates an animated beat visualization
func (tui *TUIManager) updateBeatPanel() {
	if tui.bridge.link == nil {
		return
	}
	
	tui.bridge.link.CaptureAppSessionState(tui.bridge.state)
	currentTime := tui.bridge.link.ClockMicros()
	
	// Get current musical position
	currentBeat := tui.bridge.state.BeatAtTime(currentTime, defaultQuantum)
	phase := tui.bridge.state.PhaseAtTime(currentTime, defaultQuantum) // Use 4.0 quantum for slower progress
	
	// Calculate bar and beat within bar
	beatsPerBar := float64(tui.bridge.beatsPerBar)
	bar := int(currentBeat / beatsPerBar)
	beatInBar := currentBeat - float64(bar)*beatsPerBar
	currentBeatNum := int(beatInBar)
	
	// Create large ASCII art beat display
	beatDisplay := "\n"
	
	// Bar number display
	beatDisplay += fmt.Sprintf("[yellow::]  BAR %d  [white::]", bar+1)
	beatDisplay += "\n\n"
	
	// Beat indicators with animation
	beatLine := "  "
	for i := 0; i < tui.bridge.beatsPerBar; i++ {
		if i == currentBeatNum {
			// Animate current beat based on phase
			if tui.bridge.linkIsPlaying {
				if phase < 0.25 {
					beatLine += "[red::b]●[white::] "
				} else if phase < 0.5 {
					beatLine += "[yellow::b]●[white::] "
				} else if phase < 0.75 {
					beatLine += "[green::b]●[white::] "
				} else {
					beatLine += "[blue::b]●[white::] "
				}
			} else {
				beatLine += "[gray::b]●[white::] "
			}
		} else {
			beatLine += "[darkgray::]○[white::] "
		}
	}
	beatDisplay += beatLine + "\n\n"
	
	// Beat and phase numbers
	beatDisplay += fmt.Sprintf("[cyan]Beat:[white] %.2f\n", beatInBar+1)
	beatDisplay += fmt.Sprintf("[cyan]Phase:[white] %.3f", phase)
	
	// Phase progress bar
	progressWidth := 20
	normalizedPhase := phase / defaultQuantum // Normalize to 0-1 range
	progressFilled := int(normalizedPhase * float64(progressWidth))
	progressBar := "\n["
	for i := 0; i < progressWidth; i++ {
		if i < progressFilled {
			progressBar += "[green::]█[white::]"
		} else {
			progressBar += "[darkgray::]░[white::]"
		}
	}
	progressBar += fmt.Sprintf("] %6.1f%%", normalizedPhase*100)
	beatDisplay += progressBar
	
	tui.beatPanel.SetText(beatDisplay)
}

// updateTempoPanel shows tempo and timing information
func (tui *TUIManager) updateTempoPanel() {
	tui.bridge.mu.RLock()
	linkTempo := tui.bridge.lastLinkTempo
	midiTempo := tui.bridge.lastMIDITempo
	tui.bridge.mu.RUnlock()
	
	tempoDisplay := "\n"
	
	// Link tempo (always shown)
	tempoDisplay += fmt.Sprintf("[green]Link Tempo[white]\n")
	tempoDisplay += fmt.Sprintf("[white::b]%.1f[white::] BPM\n\n", linkTempo)
	
	// MIDI tempo (if available)
	if midiTempo > 0 {
		tempoDisplay += fmt.Sprintf("[yellow]MIDI Tempo[white]\n")
		tempoDisplay += fmt.Sprintf("[white::b]%.1f[white::] BPM\n\n", midiTempo)
		
		// Tempo difference
		diff := abs(linkTempo - midiTempo)
		color := "[green]"
		if diff > 0.5 {
			color = "[red]"
		} else if diff > 0.1 {
			color = "[yellow]"
		}
		tempoDisplay += fmt.Sprintf("[cyan]Difference[white]\n")
		tempoDisplay += fmt.Sprintf("%s%.2f[white] BPM", color, diff)
	} else {
		tempoDisplay += "[darkgray]No MIDI tempo"
	}
	
	tui.tempoPanel.SetText(tempoDisplay)
}

// updateSyncPanel shows synchronization status and phase information
func (tui *TUIManager) updateSyncPanel() {
	tui.bridge.mu.RLock()
	autoPhase := tui.bridge.phaseOffset
	manualPhase := tui.bridge.manualPhaseOffset
	tui.bridge.mu.RUnlock()
	
	syncDisplay := ""
	
	// Phase offset information
	totalPhase := autoPhase + manualPhase
	
	if autoPhase != 0 || manualPhase != 0 {
		syncDisplay += "[yellow]Phase Corrections:[white]\n\n"
		
		if autoPhase != 0 {
			color := "[green]"
			if abs(float64(autoPhase.Microseconds())) > 1000 {
				color = "[red]"
			}
			syncDisplay += fmt.Sprintf("Auto: %s%+.2fms[white]\n", color, float64(autoPhase.Microseconds())/1000.0)
		}
		
		if manualPhase != 0 {
			syncDisplay += fmt.Sprintf("Manual: [cyan]%+.2fms[white]\n", float64(manualPhase.Microseconds())/1000.0)
		}
		
		if autoPhase != 0 && manualPhase != 0 {
			totalColor := "[green]"
			if abs(float64(totalPhase.Microseconds())) > 2000 {
				totalColor = "[red]"
			}
			syncDisplay += fmt.Sprintf("Total: %s%+.2fms[white]\n", totalColor, float64(totalPhase.Microseconds())/1000.0)
		}
	} else {
		syncDisplay += "[green]Perfect Sync[white]\n"
		syncDisplay += "No phase corrections needed"
	}
	
	// Connection info
	if tui.bridge.link != nil {
		peers := tui.bridge.link.NumPeers()
		syncDisplay += fmt.Sprintf("\n\n[cyan]Link Peers:[white] %d", peers)
	}
	
	tui.syncPanel.SetText(syncDisplay)
}

// GetLogWriter returns a writer for log output
func (tui *TUIManager) GetLogWriter() *TUILogWriter {
	return &TUILogWriter{tui: tui}
}

// TUILogWriter implements io.Writer for log output
type TUILogWriter struct {
	tui *TUIManager
}

func (w *TUILogWriter) Write(p []byte) (n int, err error) {
	// Format log message with timestamp
	timestamp := time.Now().Format("15:04:05")
	message := fmt.Sprintf("[darkgray]%s[white] %s", timestamp, string(p))
	
	// Remove trailing newline to avoid double spacing
	message = strings.TrimRight(message, "\n")
	
	if w.tui.logPanel != nil {
		fmt.Fprintln(w.tui.logPanel, message)
	}
	return len(p), nil
}

// Run starts the TUI application
func (tui *TUIManager) Run() error {
	return tui.app.Run()
}

// Stop gracefully shuts down the TUI
func (tui *TUIManager) Stop() {
	if tui.updateTicker != nil {
		tui.updateTicker.Stop()
	}
	close(tui.stopUpdate)
	tui.app.Stop()
}