package abletonlink

/*
#include <abl_link.h>
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