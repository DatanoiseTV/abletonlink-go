package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// EurorackTUIManager handles the terminal user interface for Eurorack bridge
type EurorackTUIManager struct {
	app            *tview.Application
	pages          *tview.Pages
	bridge         *EurorackLinkBridge
	
	// Main layout components
	headerBar      *tview.TextView
	statusPanel    *tview.Table
	clockPanel     *tview.TextView
	gpioPanel      *tview.Table
	linkPanel      *tview.TextView
	logPanel       *tview.TextView
	footerBar      *tview.TextView
	
	// Modal components
	helpModal      *tview.Modal
	
	// Update control
	updateTicker   *time.Ticker
	stopUpdate     chan bool
}

// NewEurorackTUIManager creates a new TUI manager for Eurorack
func NewEurorackTUIManager(bridge *EurorackLinkBridge) *EurorackTUIManager {
	tui := &EurorackTUIManager{
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

// setupComponents creates all the UI components
func (tui *EurorackTUIManager) setupComponents() {
	// Header bar
	tui.headerBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[white:blue:b] Eurorack-Link Bridge v1.0 [::-] [yellow]● RUNNING")
	
	// Status panel
	tui.statusPanel = tview.NewTable().
		SetBorders(false).
		SetSelectable(false, false)
	tui.statusPanel.SetTitle(" System Status ").SetBorder(true)
	
	// Clock visualization panel
	tui.clockPanel = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetWrap(false)
	tui.clockPanel.SetTitle(" Clock & Timing ").SetBorder(true)
	
	// GPIO status panel
	tui.gpioPanel = tview.NewTable().
		SetBorders(false).
		SetSelectable(false, false)
	tui.gpioPanel.SetTitle(" GPIO Status ").SetBorder(true)
	
	// Link information panel
	tui.linkPanel = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true)
	tui.linkPanel.SetTitle(" Link Network ").SetBorder(true)
	
	// Log panel
	tui.logPanel = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetChangedFunc(func() {
			tui.logPanel.ScrollToEnd()
			tui.app.Draw()
		})
	tui.logPanel.SetTitle(" Log Messages ").SetBorder(true)
	
	// Footer
	tui.footerBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[black:white] Space [black:white] Start/Stop [black:white] R [black:white] Reset [black:white] H [black:white] Help [black:white] Q [black:white] Quit ")
	
	// Help modal
	tui.helpModal = tview.NewModal().
		SetText("Eurorack-Link Bridge Controls\n\n" +
			"Space: Toggle Link transport\n" +
			"R: Send reset pulse\n" +
			"H: Show/hide this help\n" +
			"Q or Esc: Quit application\n\n" +
			"GPIO Inputs: Clock, Start, Stop, Reset\n" +
			"GPIO Outputs: 1/2/4/24 PPQN + Transport").
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			tui.pages.HidePage("help")
		})
}

// setupLayout creates the application layout
func (tui *EurorackTUIManager) setupLayout() {
	// Top row: Status and Clock
	topRow := tview.NewFlex().
		AddItem(tui.statusPanel, 0, 1, false).
		AddItem(tui.clockPanel, 0, 1, false)
	
	// Middle row: GPIO and Link
	middleRow := tview.NewFlex().
		AddItem(tui.gpioPanel, 0, 1, false).
		AddItem(tui.linkPanel, 0, 1, false)
	
	// Main content
	mainContent := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(topRow, 10, 0, false).
		AddItem(middleRow, 8, 0, false).
		AddItem(tui.logPanel, 0, 1, false)
	
	// Full layout
	fullLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tui.headerBar, 1, 0, false).
		AddItem(mainContent, 0, 1, true).
		AddItem(tui.footerBar, 1, 0, false)
	
	// Add pages
	tui.pages.AddPage("main", fullLayout, true, true)
	tui.pages.AddPage("help", tui.helpModal, true, false)
	
	// Set root
	tui.app.SetRoot(tui.pages, true)
}

// setupKeyBindings configures key handlers
func (tui *EurorackTUIManager) setupKeyBindings() {
	tui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Rune() == 'q' || event.Rune() == 'Q' || event.Key() == tcell.KeyEscape:
			name, _ := tui.pages.GetFrontPage()
			if name == "help" {
				tui.pages.HidePage("help")
				return nil
			}
			tui.Stop()
			return nil
			
		case event.Rune() == 'h' || event.Rune() == 'H':
			name, _ := tui.pages.GetFrontPage()
			if name == "help" {
				tui.pages.HidePage("help")
			} else {
				tui.pages.ShowPage("help")
			}
			return nil
			
		case event.Rune() == ' ':
			// Toggle Link transport
			tui.bridge.link.CaptureAppSessionState(tui.bridge.state)
			isPlaying := tui.bridge.state.IsPlaying()
			currentTime := tui.bridge.link.ClockMicros()
			tui.bridge.state.SetIsPlaying(!isPlaying, uint64(currentTime))
			tui.bridge.link.CommitAppSessionState(tui.bridge.state)
			return nil
			
		case event.Rune() == 'r' || event.Rune() == 'R':
			// Send reset pulse
			tui.bridge.handleExternalReset(time.Now())
			return nil
		}
		
		return event
	})
}

// startUpdateLoop begins the UI update routine
func (tui *EurorackTUIManager) startUpdateLoop() {
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
func (tui *EurorackTUIManager) updateAllPanels() {
	tui.updateStatusPanel()
	tui.updateClockPanel()
	tui.updateGPIOPanel()
	tui.updateLinkPanel()
}

// updateStatusPanel refreshes status information
func (tui *EurorackTUIManager) updateStatusPanel() {
	tui.statusPanel.Clear()
	
	row := 0
	
	// Sync mode
	syncMode := "[green]Link Master"
	if tui.bridge.externalSyncEnabled {
		syncMode = "[yellow]External Master"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Sync Mode:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(syncMode))
	row++
	
	// Transport status
	tui.bridge.link.CaptureAppSessionState(tui.bridge.state)
	linkPlaying := tui.bridge.state.IsPlaying()
	
	transportStatus := "[red]Stopped"
	if linkPlaying {
		transportStatus = "[green]Playing"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Transport:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(transportStatus))
	row++
	
	// Tempo
	tui.bridge.mu.RLock()
	linkTempo := tui.bridge.lastLinkTempo
	externalTempo := tui.bridge.lastExternalTempo
	tui.bridge.mu.RUnlock()
	
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("Link Tempo:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("[cyan]%.1f BPM", linkTempo)))
	row++
	
	if tui.bridge.externalSyncEnabled && externalTempo > 0 {
		tui.statusPanel.SetCell(row, 0, tview.NewTableCell("External Tempo:"))
		tui.statusPanel.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("[yellow]%.1f BPM", externalTempo)))
		row++
	}
	
	// GPIO status
	gpioStatus := "[red]Simulated"
	if !tui.bridge.dryRun {
		gpioStatus = "[green]Hardware"
	}
	tui.statusPanel.SetCell(row, 0, tview.NewTableCell("GPIO Mode:"))
	tui.statusPanel.SetCell(row, 1, tview.NewTableCell(gpioStatus))
}

// updateClockPanel shows clock visualization
func (tui *EurorackTUIManager) updateClockPanel() {
	tui.bridge.link.CaptureAppSessionState(tui.bridge.state)
	currentTime := tui.bridge.link.ClockMicros()
	
	// Calculate current beat and phase
	currentBeat := tui.bridge.state.BeatAtTime(currentTime, defaultQuantum)
	phase := tui.bridge.state.PhaseAtTime(currentTime, defaultQuantum)
	
	// Calculate bar and beat within bar
	beatsPerBar := float64(tui.bridge.beatsPerBar)
	bar := int(currentBeat / beatsPerBar)
	beatInBar := currentBeat - float64(bar)*beatsPerBar
	currentBeatNum := int(beatInBar)
	
	clockDisplay := "\n"
	
	// Bar and beat display
	clockDisplay += fmt.Sprintf("[yellow]BAR %d[white]\n", bar+1)
	
	// Beat indicators
	beatLine := ""
	for i := 0; i < tui.bridge.beatsPerBar; i++ {
		if i == currentBeatNum && tui.bridge.state.IsPlaying() {
			// Animate current beat
			if phase < 0.5 {
				beatLine += "[red::b]●[white::] "
			} else {
				beatLine += "[green::b]●[white::] "
			}
		} else {
			beatLine += "[darkgray::]○[white::] "
		}
	}
	clockDisplay += beatLine + "\n\n"
	
	// Beat counter
	clockDisplay += fmt.Sprintf("[cyan]Beat:[white] %.2f\n", beatInBar+1)
	
	// Phase display
	clockDisplay += fmt.Sprintf("[cyan]Phase:[white] %.3f", phase/defaultQuantum)
	
	tui.clockPanel.SetText(clockDisplay)
}

// updateGPIOPanel shows GPIO pin status
func (tui *EurorackTUIManager) updateGPIOPanel() {
	tui.gpioPanel.Clear()
	
	row := 0
	
	// Input pins
	tui.gpioPanel.SetCell(row, 0, tview.NewTableCell("[yellow]INPUTS"))
	tui.gpioPanel.SetCell(row, 1, tview.NewTableCell(""))
	row++
	
	inputs := map[string]int{
		"Clock":  tui.bridge.pins.ClockIn,
		"Start":  tui.bridge.pins.StartIn,
		"Stop":   tui.bridge.pins.StopIn,
		"Reset":  tui.bridge.pins.ResetIn,
	}
	
	for name, pin := range inputs {
		tui.gpioPanel.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("%s:", name)))
		tui.gpioPanel.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("[cyan]GPIO %d", pin)))
		row++
	}
	
	// Separator
	tui.gpioPanel.SetCell(row, 0, tview.NewTableCell(""))
	tui.gpioPanel.SetCell(row, 1, tview.NewTableCell(""))
	row++
	
	// Output pins
	tui.gpioPanel.SetCell(row, 0, tview.NewTableCell("[green]OUTPUTS"))
	tui.gpioPanel.SetCell(row, 1, tview.NewTableCell(""))
	row++
	
	outputs := map[string]int{
		"1 PPQN":   tui.bridge.pins.Clock1PPQN,
		"2 PPQN":   tui.bridge.pins.Clock2PPQN,
		"4 PPQN":   tui.bridge.pins.Clock4PPQN,
		"24 PPQN":  tui.bridge.pins.Clock24PPQN,
		"Start":    tui.bridge.pins.StartOut,
		"Stop":     tui.bridge.pins.StopOut,
		"Reset":    tui.bridge.pins.ResetOut,
	}
	
	for name, pin := range outputs {
		// Show recent pulse activity
		tui.bridge.mu.RLock()
		lastPulse, hasActivity := tui.bridge.lastPulses[strings.ToLower(strings.Replace(name, " ", "", -1))]
		tui.bridge.mu.RUnlock()
		
		status := fmt.Sprintf("[cyan]GPIO %d", pin)
		if hasActivity && time.Since(lastPulse) < 100*time.Millisecond {
			status += " [red]●"
		}
		
		tui.gpioPanel.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("%s:", name)))
		tui.gpioPanel.SetCell(row, 1, tview.NewTableCell(status))
		row++
	}
}

// updateLinkPanel shows Link network information
func (tui *EurorackTUIManager) updateLinkPanel() {
	linkInfo := ""
	
	// Link peers
	peers := tui.bridge.link.NumPeers()
	linkInfo += fmt.Sprintf("[cyan]Connected Peers:[white] %d\n\n", peers)
	
	// Link status
	linkInfo += "[cyan]Link Session:[white]\n"
	linkInfo += fmt.Sprintf("• Enabled: [green]Yes[white]\n")
	linkInfo += fmt.Sprintf("• Start/Stop Sync: [green]Yes[white]\n")
	
	// External sync info
	if tui.bridge.externalSyncEnabled {
		tui.bridge.mu.RLock()
		clockCount := tui.bridge.externalClockCount
		tui.bridge.mu.RUnlock()
		
		linkInfo += "\n[yellow]External Sync:[white]\n"
		linkInfo += fmt.Sprintf("• Clock Count: [yellow]%d[white]\n", clockCount)
		linkInfo += fmt.Sprintf("• Mode: [yellow]Active[white]")
	} else {
		linkInfo += "\n[darkgray]External Sync: Disabled"
	}
	
	tui.linkPanel.SetText(linkInfo)
}

// GetLogWriter returns a writer for log output
func (tui *EurorackTUIManager) GetLogWriter() *EurorackLogWriter {
	return &EurorackLogWriter{tui: tui}
}

// EurorackLogWriter implements io.Writer for log output
type EurorackLogWriter struct {
	tui *EurorackTUIManager
}

func (w *EurorackLogWriter) Write(p []byte) (n int, err error) {
	// Parse structured log message
	logLine := strings.TrimSpace(string(p))
	
	// Extract message part from logrus format
	if strings.Contains(logLine, "msg=") {
		parts := strings.Split(logLine, "msg=")
		if len(parts) > 1 {
			msg := strings.Trim(parts[1], "\"")
			timestamp := time.Now().Format("15:04:05")
			message := fmt.Sprintf("[darkgray]%s[white] %s", timestamp, msg)
			
			if w.tui.logPanel != nil {
				fmt.Fprintln(w.tui.logPanel, message)
			}
		}
	} else {
		// Fallback for non-structured logs
		timestamp := time.Now().Format("15:04:05")
		message := fmt.Sprintf("[darkgray]%s[white] %s", timestamp, logLine)
		
		if w.tui.logPanel != nil {
			fmt.Fprintln(w.tui.logPanel, message)
		}
	}
	
	return len(p), nil
}

// Run starts the TUI application
func (tui *EurorackTUIManager) Run() error {
	return tui.app.Run()
}

// Stop gracefully shuts down the TUI
func (tui *EurorackTUIManager) Stop() {
	if tui.updateTicker != nil {
		tui.updateTicker.Stop()
	}
	close(tui.stopUpdate)
	tui.app.Stop()
}