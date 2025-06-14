//go:build !static
//go:generate go run generate.go

package abletonlink

/*
#cgo CPPFLAGS: -I${SRCDIR}/vendor/link/include -I${SRCDIR}/vendor/link/extensions/abl_link/include
#cgo LDFLAGS: -L${SRCDIR}/vendor/link/build -labl_link -lstdc++ -lpthread

#include <abl_link.h>
#include <stdlib.h>

// Forward declarations for Go callbacks
void go_num_peers_callback(uint64_t num_peers, void *context);
void go_tempo_callback(double tempo, void *context);
void go_start_stop_callback(bool is_playing, void *context);

// C wrapper functions - these must be defined here, not just declared
void c_num_peers_callback(uint64_t num_peers, void *context) {
    go_num_peers_callback(num_peers, context);
}

void c_tempo_callback(double tempo, void *context) {
    go_tempo_callback(tempo, context);
}

void c_start_stop_callback(bool is_playing, void *context) {
    go_start_stop_callback(is_playing, context);
}
*/
import "C"
import (
	"sync"
	"unsafe"
)

// Global storage for Go callbacks to avoid CGO pointer issues
var (
	linkRegistry = make(map[uintptr]*Link)
	linkRegistryMu sync.RWMutex
	nextLinkID uintptr = 1
)

// Link represents an Ableton Link instance
type Link struct {
	impl C.abl_link
	mu   sync.Mutex
	id   uintptr  // C-safe identifier

	numPeersCallback  func(uint64)
	tempoCallback     func(float64)
	startStopCallback func(bool)
	captureStateMu   sync.Mutex  // For non-thread-safe CaptureAppSessionState/CommitAppSessionState
}

// SessionState represents the current session state
type SessionState struct {
	impl C.abl_link_session_state
}

// NewLink creates a new Link instance with the given initial tempo in BPM
func NewLink(bpm float64) *Link {
	linkRegistryMu.Lock()
	id := nextLinkID
	nextLinkID++
	linkRegistryMu.Unlock()
	
	link := &Link{
		impl: C.abl_link_create(C.double(bpm)),
		id:   id,
	}
	
	// Register the link instance
	linkRegistryMu.Lock()
	linkRegistry[id] = link
	linkRegistryMu.Unlock()
	
	return link
}

// Destroy cleans up the Link instance
func (l *Link) Destroy() {
	// Unregister the link instance
	linkRegistryMu.Lock()
	delete(linkRegistry, l.id)
	linkRegistryMu.Unlock()
	
	C.abl_link_destroy(l.impl)
}

// IsEnabled returns whether Link is currently enabled
func (l *Link) IsEnabled() bool {
	return bool(C.abl_link_is_enabled(l.impl))
}

// Enable enables or disables Link
func (l *Link) Enable(enable bool) {
	C.abl_link_enable(l.impl, C.bool(enable))
}

// IsStartStopSyncEnabled returns whether start/stop sync is enabled
func (l *Link) IsStartStopSyncEnabled() bool {
	return bool(C.abl_link_is_start_stop_sync_enabled(l.impl))
}

// EnableStartStopSync enables or disables start/stop synchronization
func (l *Link) EnableStartStopSync(enable bool) {
	C.abl_link_enable_start_stop_sync(l.impl, C.bool(enable))
}

// NumPeers returns the number of currently connected peers
func (l *Link) NumPeers() uint64 {
	return uint64(C.abl_link_num_peers(l.impl))
}

// ClockMicros returns the current Link clock time in microseconds
func (l *Link) ClockMicros() int64 {
	return int64(C.abl_link_clock_micros(l.impl))
}

// SetNumPeersCallback sets a callback to be called when the number of peers changes
func (l *Link) SetNumPeersCallback(callback func(uint64)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.numPeersCallback = callback
	if callback != nil {
		C.abl_link_set_num_peers_callback(l.impl, (*[0]byte)(C.c_num_peers_callback), unsafe.Pointer(l.id))
	} else {
		C.abl_link_set_num_peers_callback(l.impl, nil, nil)
	}
}

// SetTempoCallback sets a callback to be called when the tempo changes
func (l *Link) SetTempoCallback(callback func(float64)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.tempoCallback = callback
	if callback != nil {
		C.abl_link_set_tempo_callback(l.impl, (*[0]byte)(C.c_tempo_callback), unsafe.Pointer(l.id))
	} else {
		C.abl_link_set_tempo_callback(l.impl, nil, nil)
	}
}

// SetStartStopCallback sets a callback to be called when start/stop state changes
func (l *Link) SetStartStopCallback(callback func(bool)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.startStopCallback = callback
	if callback != nil {
		C.abl_link_set_start_stop_callback(l.impl, (*[0]byte)(C.c_start_stop_callback), unsafe.Pointer(l.id))
	} else {
		C.abl_link_set_start_stop_callback(l.impl, nil, nil)
	}
}

// NewSessionState creates a new session state instance
func NewSessionState() *SessionState {
	return &SessionState{
		impl: C.abl_link_create_session_state(),
	}
}

// Destroy cleans up the session state
func (s *SessionState) Destroy() {
	C.abl_link_destroy_session_state(s.impl)
}

// CaptureAudioSessionState captures the current session state for audio thread use
func (l *Link) CaptureAudioSessionState(state *SessionState) {
	C.abl_link_capture_audio_session_state(l.impl, state.impl)
}

// CommitAudioSessionState commits session state changes from audio thread
func (l *Link) CommitAudioSessionState(state *SessionState) {
	C.abl_link_commit_audio_session_state(l.impl, state.impl)
}

// CaptureAppSessionState captures the current session state for application thread use
func (l *Link) CaptureAppSessionState(state *SessionState) {
	l.captureStateMu.Lock()
	defer l.captureStateMu.Unlock()
	C.abl_link_capture_app_session_state(l.impl, state.impl)
}

// CommitAppSessionState commits session state changes from application thread
func (l *Link) CommitAppSessionState(state *SessionState) {
	l.captureStateMu.Lock()
	defer l.captureStateMu.Unlock()
	C.abl_link_commit_app_session_state(l.impl, state.impl)
}

// Tempo returns the current tempo in BPM
func (s *SessionState) Tempo() float64 {
	return float64(C.abl_link_tempo(s.impl))
}

// SetTempo sets the tempo at the given time
func (s *SessionState) SetTempo(bpm float64, atTime int64) {
	C.abl_link_set_tempo(s.impl, C.double(bpm), C.int64_t(atTime))
}

// BeatAtTime returns the beat value at the given time for the given quantum
func (s *SessionState) BeatAtTime(time int64, quantum float64) float64 {
	return float64(C.abl_link_beat_at_time(s.impl, C.int64_t(time), C.double(quantum)))
}

// PhaseAtTime returns the phase at the given time for the given quantum
func (s *SessionState) PhaseAtTime(time int64, quantum float64) float64 {
	return float64(C.abl_link_phase_at_time(s.impl, C.int64_t(time), C.double(quantum)))
}

// TimeAtBeat returns the time at which the given beat occurs
func (s *SessionState) TimeAtBeat(beat float64, quantum float64) int64 {
	return int64(C.abl_link_time_at_beat(s.impl, C.double(beat), C.double(quantum)))
}

// RequestBeatAtTime attempts to map the given beat to the given time
func (s *SessionState) RequestBeatAtTime(beat float64, time int64, quantum float64) {
	C.abl_link_request_beat_at_time(s.impl, C.double(beat), C.int64_t(time), C.double(quantum))
}

// ForceBeatAtTime forcibly maps the given beat to the given time
func (s *SessionState) ForceBeatAtTime(beat float64, time uint64, quantum float64) {
	C.abl_link_force_beat_at_time(s.impl, C.double(beat), C.uint64_t(time), C.double(quantum))
}

// SetIsPlaying sets the transport playing state at the given time
func (s *SessionState) SetIsPlaying(isPlaying bool, time uint64) {
	C.abl_link_set_is_playing(s.impl, C.bool(isPlaying), C.uint64_t(time))
}

// IsPlaying returns whether transport is currently playing
func (s *SessionState) IsPlaying() bool {
	return bool(C.abl_link_is_playing(s.impl))
}

// TimeForIsPlaying returns the time at which transport start/stop occurs
func (s *SessionState) TimeForIsPlaying() uint64 {
	return uint64(C.abl_link_time_for_is_playing(s.impl))
}

// RequestBeatAtStartPlayingTime convenience function to map beat to start playing time
func (s *SessionState) RequestBeatAtStartPlayingTime(beat float64, quantum float64) {
	C.abl_link_request_beat_at_start_playing_time(s.impl, C.double(beat), C.double(quantum))
}

// SetIsPlayingAndRequestBeatAtTime convenience function to set playing state and map beat
func (s *SessionState) SetIsPlayingAndRequestBeatAtTime(isPlaying bool, time uint64, beat float64, quantum float64) {
	C.abl_link_set_is_playing_and_request_beat_at_time(s.impl, C.bool(isPlaying), C.uint64_t(time), C.double(beat), C.double(quantum))
}