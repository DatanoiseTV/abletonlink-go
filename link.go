//go:build !static
//go:generate go run generate.go

package abletonlink

/*
#cgo CPPFLAGS: -I${SRCDIR}/vendor/link/include -I${SRCDIR}/vendor/link/modules/asio-standalone/asio/include -I${SRCDIR}
#cgo darwin CPPFLAGS: -DLINK_PLATFORM_MACOSX=1
#cgo linux CPPFLAGS: -DLINK_PLATFORM_LINUX=1
#cgo windows CPPFLAGS: -DLINK_PLATFORM_WINDOWS=1
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -L${SRCDIR} -lstdc++ -lpthread -lm

#include "clink.h"
#include <stdlib.h>

// Forward declarations for Go callbacks
void go_num_peers_callback(uint64_t num_peers, void *context);
void go_tempo_callback(double tempo, void *context);
void go_start_stop_callback(bool is_playing, void *context);
void go_channels_changed_callback(void *context);
void go_source_callback(int16_t *samples, clink_source_buffer_info *info, void *context);

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

void c_channels_changed_callback(void *context) {
    go_channels_changed_callback(context);
}

void c_source_callback(int16_t *samples, clink_source_buffer_info *info, void *context) {
    go_source_callback(samples, info, context);
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

	sourceRegistry = make(map[uintptr]*Source)
	sourceRegistryMu sync.RWMutex
	nextSourceID uintptr = 1
)

// Link represents an Ableton Link instance
type Link struct {
	impl C.clink
	mu   sync.Mutex
	id   uintptr  // C-safe identifier

	numPeersCallback  func(uint64)
	tempoCallback     func(float64)
	startStopCallback func(bool)
	channelsChangedCallback func()
	captureStateMu   sync.Mutex  // For non-thread-safe CaptureAppSessionState/CommitAppSessionState
}

// SessionState represents the current session state
type SessionState struct {
	impl C.clink_session_state
}

// Channel represents an audio channel in the Link session
type Channel struct {
	ID       uint64
	Name     string
	PeerID   uint64
	PeerName string
}

// Sink represents a Link audio sink for sending audio
type Sink struct {
	impl C.clink_sink
}

// SinkBuffer represents a buffer for writing audio samples
type SinkBuffer struct {
	Samples       []int16_t
	MaxNumSamples uint64
	cBuffer       C.clink_sink_buffer
}

// int16_t is not a standard Go type, let's use int16
type int16_t = int16

// Source represents a Link audio source for receiving audio
type Source struct {
	impl C.clink_source
	id   uintptr
	callback func([]int16, SourceBufferInfo)
}

// SourceBufferInfo represents metadata about a received audio buffer
type SourceBufferInfo struct {
	NumChannels     uint64
	NumFrames       uint64
	SampleRate      uint32
	Count           uint64
	SessionBeatTime float64
	SessionID       uint64
	Tempo           float64
}

// NewLink creates a new Link instance with the given initial tempo in BPM
func NewLink(bpm float64) *Link {
	return NewLinkWithName(bpm, "icecast")
}

// NewLinkWithName creates a new Link instance with the given initial tempo and peer name
func NewLinkWithName(bpm float64, name string) *Link {
	linkRegistryMu.Lock()
	id := nextLinkID
	nextLinkID++
	linkRegistryMu.Unlock()
	
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	
	link := &Link{
		impl: C.clink_create(C.double(bpm), cName),
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
	
	C.clink_destroy(l.impl)
}

// IsEnabled returns whether Link is currently enabled
func (l *Link) IsEnabled() bool {
	return bool(C.clink_is_enabled(l.impl))
}

// Enable enables or disables Link
func (l *Link) Enable(enable bool) {
	C.clink_enable(l.impl, C.bool(enable))
}

// IsAudioEnabled returns whether LinkAudio is currently enabled
func (l *Link) IsAudioEnabled() bool {
	return bool(C.clink_is_audio_enabled(l.impl))
}

// EnableAudio enables or disables LinkAudio
func (l *Link) EnableAudio(enable bool) {
	C.clink_enable_audio(l.impl, C.bool(enable))
}

// SetPeerName sets the local peer name
func (l *Link) SetPeerName(name string) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	C.clink_set_peer_name(l.impl, cName)
}

// IsStartStopSyncEnabled returns whether start/stop sync is enabled
func (l *Link) IsStartStopSyncEnabled() bool {
	return bool(C.clink_is_start_stop_sync_enabled(l.impl))
}

// EnableStartStopSync enables or disables start/stop synchronization
func (l *Link) EnableStartStopSync(enable bool) {
	C.clink_enable_start_stop_sync(l.impl, C.bool(enable))
}

// NumPeers returns the number of currently connected peers
func (l *Link) NumPeers() uint64 {
	return uint64(C.clink_num_peers(l.impl))
}

// ClockMicros returns the current Link clock time in microseconds
func (l *Link) ClockMicros() int64 {
	return int64(C.clink_clock_micros(l.impl))
}

// SetNumPeersCallback sets a callback to be called when the number of peers changes
func (l *Link) SetNumPeersCallback(callback func(uint64)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.numPeersCallback = callback
	if callback != nil {
		C.clink_set_num_peers_callback(l.impl, (C.clink_num_peers_callback)(C.c_num_peers_callback), unsafe.Pointer(l.id))
	} else {
		C.clink_set_num_peers_callback(l.impl, nil, nil)
	}
}

// SetTempoCallback sets a callback to be called when the tempo changes
func (l *Link) SetTempoCallback(callback func(float64)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.tempoCallback = callback
	if callback != nil {
		C.clink_set_tempo_callback(l.impl, (C.clink_tempo_callback)(C.c_tempo_callback), unsafe.Pointer(l.id))
	} else {
		C.clink_set_tempo_callback(l.impl, nil, nil)
	}
}

// SetStartStopCallback sets a callback to be called when start/stop state changes
func (l *Link) SetStartStopCallback(callback func(bool)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.startStopCallback = callback
	if callback != nil {
		C.clink_set_start_stop_callback(l.impl, (C.clink_start_stop_callback)(C.c_start_stop_callback), unsafe.Pointer(l.id))
	} else {
		C.clink_set_start_stop_callback(l.impl, nil, nil)
	}
}

// SetChannelsChangedCallback sets a callback to be called when audio channels change
func (l *Link) SetChannelsChangedCallback(callback func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	l.channelsChangedCallback = callback
	if callback != nil {
		C.clink_set_channels_changed_callback(l.impl, (C.clink_channels_changed_callback)(C.c_channels_changed_callback), unsafe.Pointer(l.id))
	} else {
		C.clink_set_channels_changed_callback(l.impl, nil, nil)
	}
}

// Channels returns the current list of audio channels
func (l *Link) Channels() []Channel {
	cChannels := C.clink_capture_channels(l.impl)
	defer C.clink_destroy_channels(cChannels)
	
	count := uint64(C.clink_channels_count(cChannels))
	channels := make([]Channel, count)
	
	for i := uint64(0); i < count; i++ {
		channels[i] = Channel{
			ID:       uint64(C.clink_channel_id(cChannels, C.uint64_t(i))),
			Name:     C.GoString(C.clink_channel_name(cChannels, C.uint64_t(i))),
			PeerID:   uint64(C.clink_channel_peer_id(cChannels, C.uint64_t(i))),
			PeerName: C.GoString(C.clink_channel_peer_name(cChannels, C.uint64_t(i))),
		}
	}
	
	return channels
}

// NewSink creates a new audio sink
func (l *Link) NewSink(name string, maxNumSamples uint64) *Sink {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	
	return &Sink{
		impl: C.clink_create_sink(l.impl, cName, C.uint64_t(maxNumSamples)),
	}
}

// Destroy cleans up the sink
func (s *Sink) Destroy() {
	C.clink_destroy_sink(s.impl)
}

// Name returns the name of the sink
func (s *Sink) Name() string {
	return C.GoString(C.clink_sink_name(s.impl))
}

// SetName sets the name of the sink
func (s *Sink) SetName(name string) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	C.clink_sink_set_name(s.impl, cName)
}

// RequestMaxNumSamples requests a maximum buffer size for future buffers
func (s *Sink) RequestMaxNumSamples(numSamples uint64) {
	C.clink_sink_request_max_num_samples(s.impl, C.uint64_t(numSamples))
}

// MaxNumSamples returns the current maximum number of samples a buffer can hold
func (s *Sink) MaxNumSamples() uint64 {
	return uint64(C.clink_sink_max_num_samples(s.impl))
}

// RetainBuffer retains a buffer for writing audio samples
func (s *Sink) RetainBuffer() *SinkBuffer {
	var cBuffer C.clink_sink_buffer
	if bool(C.clink_sink_retain_buffer(s.impl, &cBuffer)) {
		buf := &SinkBuffer{
			MaxNumSamples: uint64(cBuffer.max_num_samples),
			cBuffer:       cBuffer,
		}
		// Create a slice from the C pointer
		buf.Samples = unsafe.Slice((*int16)(unsafe.Pointer(cBuffer.samples)), cBuffer.max_num_samples)
		return buf
	}
	return nil
}

// Commit commits the buffer after writing samples
func (s *Sink) Commit(buf *SinkBuffer, state *SessionState, beatsAtBufferBegin float64, quantum float64, numFrames uint64, numChannels uint64, sampleRate uint32) bool {
	if buf == nil || buf.cBuffer.handle == nil {
		return false
	}
	res := bool(C.clink_sink_commit_buffer(
		s.impl,
		&buf.cBuffer,
		state.impl,
		C.double(beatsAtBufferBegin),
		C.double(quantum),
		C.uint64_t(numFrames),
		C.uint64_t(numChannels),
		C.uint32_t(sampleRate),
	))
	buf.cBuffer.handle = nil
	buf.Samples = nil
	return res
}

// Release releases the buffer without committing
func (s *Sink) Release(buf *SinkBuffer) {
	if buf != nil && buf.cBuffer.handle != nil {
		C.clink_sink_release_buffer(s.impl, &buf.cBuffer)
		buf.cBuffer.handle = nil
		buf.Samples = nil
	}
}

// NewSource creates a new audio source
func (l *Link) NewSource(channelID uint64, callback func([]int16, SourceBufferInfo)) *Source {
	sourceRegistryMu.Lock()
	id := nextSourceID
	nextSourceID++
	sourceRegistryMu.Unlock()
	
	s := &Source{
		id: id,
		callback: callback,
	}
	
	sourceRegistryMu.Lock()
	sourceRegistry[id] = s
	sourceRegistryMu.Unlock()
	
	s.impl = C.clink_create_source(
		l.impl,
		C.uint64_t(channelID),
		(C.clink_source_callback)(C.c_source_callback),
		unsafe.Pointer(id),
	)
	
	return s
}

// Destroy cleans up the source
func (s *Source) Destroy() {
	sourceRegistryMu.Lock()
	delete(sourceRegistry, s.id)
	sourceRegistryMu.Unlock()
	
	C.clink_destroy_source(s.impl)
}

// ChannelID returns the channel ID of the source
func (s *Source) ChannelID() uint64 {
	return uint64(C.clink_source_channel_id(s.impl))
}

// BeginBeats maps the beat time at the begin of the buffer to the local Link session state
func (i SourceBufferInfo) BeginBeats(state *SessionState, quantum float64) (float64, bool) {
	var outBeats C.double
	c_info := C.clink_source_buffer_info{
		num_channels:      C.uint64_t(i.NumChannels),
		num_frames:        C.uint64_t(i.NumFrames),
		sample_rate:       C.uint32_t(i.SampleRate),
		count:             C.uint64_t(i.Count),
		session_beat_time: C.double(i.SessionBeatTime),
		session_id:        C.uint64_t(i.SessionID),
		tempo:             C.double(i.Tempo),
	}
	
	ok := bool(C.clink_source_info_begin_beats(&c_info, state.impl, C.double(quantum), &outBeats))
	return float64(outBeats), ok
}

// EndBeats maps the beat time at the end of the buffer to the local Link session state
func (i SourceBufferInfo) EndBeats(state *SessionState, quantum float64) (float64, bool) {
	var outBeats C.double
	c_info := C.clink_source_buffer_info{
		num_channels:      C.uint64_t(i.NumChannels),
		num_frames:        C.uint64_t(i.NumFrames),
		sample_rate:       C.uint32_t(i.SampleRate),
		count:             C.uint64_t(i.Count),
		session_beat_time: C.double(i.SessionBeatTime),
		session_id:        C.uint64_t(i.SessionID),
		tempo:             C.double(i.Tempo),
	}
	
	ok := bool(C.clink_source_info_end_beats(&c_info, state.impl, C.double(quantum), &outBeats))
	return float64(outBeats), ok
}

// NewSessionState creates a new session state instance
func NewSessionState() *SessionState {
	return &SessionState{
		impl: C.clink_create_session_state(),
	}
}

// Destroy cleans up the session state
func (s *SessionState) Destroy() {
	C.clink_destroy_session_state(s.impl)
}

// CaptureAudioSessionState captures the current session state for audio thread use
func (l *Link) CaptureAudioSessionState(state *SessionState) {
	C.clink_capture_audio_session_state(l.impl, state.impl)
}

// CommitAudioSessionState commits session state changes from audio thread
func (l *Link) CommitAudioSessionState(state *SessionState) {
	C.clink_commit_audio_session_state(l.impl, state.impl)
}

// CaptureAppSessionState captures the current session state for application thread use
func (l *Link) CaptureAppSessionState(state *SessionState) {
	l.captureStateMu.Lock()
	defer l.captureStateMu.Unlock()
	C.clink_capture_app_session_state(l.impl, state.impl)
}

// CommitAppSessionState commits session state changes from application thread
func (l *Link) CommitAppSessionState(state *SessionState) {
	l.captureStateMu.Lock()
	defer l.captureStateMu.Unlock()
	C.clink_commit_app_session_state(l.impl, state.impl)
}

// Tempo returns the current tempo in BPM
func (s *SessionState) Tempo() float64 {
	return float64(C.clink_tempo(s.impl))
}

// SetTempo sets the tempo at the given time
func (s *SessionState) SetTempo(bpm float64, atTime int64) {
	C.clink_set_tempo(s.impl, C.double(bpm), C.int64_t(atTime))
}

// BeatAtTime returns the beat value at the given time for the given quantum
func (s *SessionState) BeatAtTime(time int64, quantum float64) float64 {
	return float64(C.clink_beat_at_time(s.impl, C.int64_t(time), C.double(quantum)))
}

// PhaseAtTime returns the phase at the given time for the given quantum
func (s *SessionState) PhaseAtTime(time int64, quantum float64) float64 {
	return float64(C.clink_phase_at_time(s.impl, C.int64_t(time), C.double(quantum)))
}

// TimeAtBeat returns the time at which the given beat occurs
func (s *SessionState) TimeAtBeat(beat float64, quantum float64) int64 {
	return int64(C.clink_time_at_beat(s.impl, C.double(beat), C.double(quantum)))
}

// RequestBeatAtTime attempts to map the given beat to the given time
func (s *SessionState) RequestBeatAtTime(beat float64, time int64, quantum float64) {
	C.clink_request_beat_at_time(s.impl, C.double(beat), C.int64_t(time), C.double(quantum))
}

// ForceBeatAtTime forcibly maps the given beat to the given time
func (s *SessionState) ForceBeatAtTime(beat float64, time int64, quantum float64) {
	C.clink_force_beat_at_time(s.impl, C.double(beat), C.int64_t(time), C.double(quantum))
}

// SetIsPlaying sets the transport playing state at the given time
func (s *SessionState) SetIsPlaying(isPlaying bool, time int64) {
	C.clink_set_is_playing(s.impl, C.bool(isPlaying), C.int64_t(time))
}

// IsPlaying returns whether transport is currently playing
func (s *SessionState) IsPlaying() bool {
	return bool(C.clink_is_playing(s.impl))
}

// TimeForIsPlaying returns the time at which transport start/stop occurs
func (s *SessionState) TimeForIsPlaying() int64 {
	return int64(C.clink_time_for_is_playing(s.impl))
}

// RequestBeatAtStartPlayingTime convenience function to map beat to start playing time
func (s *SessionState) RequestBeatAtStartPlayingTime(beat float64, quantum float64) {
	C.clink_request_beat_at_start_playing_time(s.impl, C.double(beat), C.double(quantum))
}

// SetIsPlayingAndRequestBeatAtTime convenience function to set playing state and map beat
func (s *SessionState) SetIsPlayingAndRequestBeatAtTime(isPlaying bool, time int64, beat float64, quantum float64) {
	C.clink_set_is_playing_and_request_beat_at_time(s.impl, C.bool(isPlaying), C.int64_t(time), C.double(beat), C.double(quantum))
}
