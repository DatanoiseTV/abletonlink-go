package abletonlink

/*
#include <abl_link.h>
*/
import "C"
import "unsafe"

// Callback functions called from C
//export go_num_peers_callback
func go_num_peers_callback(numPeers C.uint64_t, context unsafe.Pointer) {
	if context == nil {
		return
	}
	
	link := (*Link)(context)
	link.mu.Lock()
	callback := link.numPeersCallback
	link.mu.Unlock()
	
	if callback != nil {
		callback(uint64(numPeers))
	}
}

//export go_tempo_callback
func go_tempo_callback(tempo C.double, context unsafe.Pointer) {
	if context == nil {
		return
	}
	
	link := (*Link)(context)
	link.mu.Lock()
	callback := link.tempoCallback
	link.mu.Unlock()
	
	if callback != nil {
		callback(float64(tempo))
	}
}

//export go_start_stop_callback
func go_start_stop_callback(isPlaying C.bool, context unsafe.Pointer) {
	if context == nil {
		return
	}
	
	link := (*Link)(context)
	link.mu.Lock()
	callback := link.startStopCallback
	link.mu.Unlock()
	
	if callback != nil {
		callback(bool(isPlaying))
	}
}