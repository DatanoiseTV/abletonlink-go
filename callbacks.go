package abletonlink

/*
#include "clink.h"
*/
import "C"
import "unsafe"

// Callback functions called from C - using registry to avoid CGO pointer issues
//export go_num_peers_callback
func go_num_peers_callback(numPeers C.uint64_t, context unsafe.Pointer) {
	id := uintptr(context)
	linkRegistryMu.RLock()
	link, exists := linkRegistry[id]
	linkRegistryMu.RUnlock()
	
	if exists && link.numPeersCallback != nil {
		link.numPeersCallback(uint64(numPeers))
	}
}

//export go_tempo_callback
func go_tempo_callback(tempo C.double, context unsafe.Pointer) {
	id := uintptr(context)
	linkRegistryMu.RLock()
	link, exists := linkRegistry[id]
	linkRegistryMu.RUnlock()
	
	if exists && link.tempoCallback != nil {
		link.tempoCallback(float64(tempo))
	}
}

//export go_start_stop_callback
func go_start_stop_callback(isPlaying C.bool, context unsafe.Pointer) {
	id := uintptr(context)
	linkRegistryMu.RLock()
	link, exists := linkRegistry[id]
	linkRegistryMu.RUnlock()
	
	if exists && link.startStopCallback != nil {
		link.startStopCallback(bool(isPlaying))
	}
}

//export go_channels_changed_callback
func go_channels_changed_callback(context unsafe.Pointer) {
	id := uintptr(context)
	linkRegistryMu.RLock()
	link, exists := linkRegistry[id]
	linkRegistryMu.RUnlock()
	
	if exists && link.channelsChangedCallback != nil {
		link.channelsChangedCallback()
	}
}

//export go_source_callback
func go_source_callback(samples *C.int16_t, info *C.clink_source_buffer_info, context unsafe.Pointer) {
	id := uintptr(context)
	sourceRegistryMu.RLock()
	source, exists := sourceRegistry[id]
	sourceRegistryMu.RUnlock()
	
	if exists && source.callback != nil {
		numSamples := uint64(info.num_frames) * uint64(info.num_channels)
		samplesSlice := unsafe.Slice((*int16)(unsafe.Pointer(samples)), numSamples)
		
		goInfo := SourceBufferInfo{
			NumChannels:     uint64(info.num_channels),
			NumFrames:       uint64(info.num_frames),
			SampleRate:      uint32(info.sample_rate),
			Count:           uint64(info.count),
			SessionBeatTime: float64(info.session_beat_time),
			SessionID:       uint64(info.session_id),
			Tempo:           float64(info.tempo),
		}
		
		source.callback(samplesSlice, goInfo)
	}
}
