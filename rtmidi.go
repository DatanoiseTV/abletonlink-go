package abletonlink

/*
#cgo pkg-config: rtmidi
#include <rtmidi_c.h>
#include <stdlib.h>

// Forward declaration for Go callback
extern void rtmidi_callback(double timestamp, unsigned char* message, size_t messageSize, void* userData);
*/
import "C"
import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Global registry for MIDI callbacks to avoid CGO pointer issues
var (
	midiCallbackRegistry   = make(map[uintptr]func([]byte))
	midiCallbackRegistryMu sync.RWMutex
	nextMidiID             uintptr = 1
)

// MidiIn represents an RtMidi input device
type MidiIn struct {
	ptr C.RtMidiInPtr
	id  uintptr
}

// MidiOut represents an RtMidi output device
type MidiOut struct {
	ptr C.RtMidiOutPtr
}

// NewMidiIn creates a new MIDI input
func NewMidiIn() (*MidiIn, error) {
	ptr := C.rtmidi_in_create_default()
	if ptr == nil {
		return nil, fmt.Errorf("failed to create MIDI input")
	}

	midiCallbackRegistryMu.Lock()
	id := nextMidiID
	nextMidiID++
	midiCallbackRegistryMu.Unlock()

	in := &MidiIn{ptr: ptr, id: id}
	runtime.SetFinalizer(in, (*MidiIn).Close)
	return in, nil
}

// NewMidiOut creates a new MIDI output
func NewMidiOut() (*MidiOut, error) {
	ptr := C.rtmidi_out_create_default()
	if ptr == nil {
		return nil, fmt.Errorf("failed to create MIDI output")
	}

	out := &MidiOut{ptr: ptr}
	runtime.SetFinalizer(out, (*MidiOut).Close)
	return out, nil
}

// OpenVirtualPort opens a virtual MIDI input port
func (in *MidiIn) OpenVirtualPort(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	C.rtmidi_open_virtual_port(in.ptr, cname)

	if !in.ptr.ok {
		return fmt.Errorf("failed to open virtual input port '%s': %s", name, C.GoString(in.ptr.msg))
	}
	return nil
}

// OpenVirtualPort opens a virtual MIDI output port
func (out *MidiOut) OpenVirtualPort(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	C.rtmidi_open_virtual_port(out.ptr, cname)

	if !out.ptr.ok {
		return fmt.Errorf("failed to open virtual output port '%s': %s", name, C.GoString(out.ptr.msg))
	}
	return nil
}

// SetCallback sets a callback function for incoming MIDI messages
func (in *MidiIn) SetCallback(callback func([]byte)) {
	midiCallbackRegistryMu.Lock()
	midiCallbackRegistry[in.id] = callback
	midiCallbackRegistryMu.Unlock()

	C.rtmidi_in_set_callback(in.ptr, (*[0]byte)(C.rtmidi_callback), unsafe.Pointer(in.id))
}

// IgnoreTypes configures which MIDI message types to ignore
// midiSysex: ignore system exclusive messages
// midiTime: ignore MIDI time messages (including clock)
// midiSense: ignore active sensing messages
func (in *MidiIn) IgnoreTypes(midiSysex, midiTime, midiSense bool) {
	C.rtmidi_in_ignore_types(in.ptr, C.bool(midiSysex), C.bool(midiTime), C.bool(midiSense))
}

// OpenPort opens a physical MIDI input port by number
func (in *MidiIn) OpenPort(portNumber uint, portName string) error {
	cname := C.CString(portName)
	defer C.free(unsafe.Pointer(cname))

	C.rtmidi_open_port(in.ptr, C.uint(portNumber), cname)

	if !in.ptr.ok {
		return fmt.Errorf("failed to open input port %d '%s': %s", portNumber, portName, C.GoString(in.ptr.msg))
	}
	return nil
}

// OpenPort opens a physical MIDI output port by number
func (out *MidiOut) OpenPort(portNumber uint, portName string) error {
	cname := C.CString(portName)
	defer C.free(unsafe.Pointer(cname))

	C.rtmidi_open_port(out.ptr, C.uint(portNumber), cname)

	if !out.ptr.ok {
		return fmt.Errorf("failed to open output port %d '%s': %s", portNumber, portName, C.GoString(out.ptr.msg))
	}
	return nil
}

// GetPortCount returns the number of available MIDI input ports
func (in *MidiIn) GetPortCount() uint {
	return uint(C.rtmidi_get_port_count(in.ptr))
}

// GetPortCount returns the number of available MIDI output ports
func (out *MidiOut) GetPortCount() uint {
	return uint(C.rtmidi_get_port_count(out.ptr))
}

// GetPortName returns the name of a MIDI input port
func (in *MidiIn) GetPortName(portNumber uint) string {
	// Check if the wrapper is valid
	if !in.ptr.ok {
		return fmt.Sprintf("Port %d (error)", portNumber)
	}

	// First call to get required buffer length
	var bufLen C.int = 0
	result := C.rtmidi_get_port_name(in.ptr, C.uint(portNumber), nil, &bufLen)
	if result != 0 || bufLen <= 0 {
		return fmt.Sprintf("Port %d (no name)", portNumber)
	}
	
	// Allocate buffer with extra space for null terminator
	buf := (*C.char)(C.malloc(C.size_t(bufLen + 1)))
	defer C.free(unsafe.Pointer(buf))
	
	// Zero the buffer
	for i := 0; i < int(bufLen+1); i++ {
		*(*C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(buf)) + uintptr(i))) = 0
	}
	
	result = C.rtmidi_get_port_name(in.ptr, C.uint(portNumber), buf, &bufLen)
	if result == 0 {
		name := C.GoString(buf)
		if name != "" {
			return name
		}
	}
	return fmt.Sprintf("Port %d", portNumber)
}

// GetPortName returns the name of a MIDI output port
func (out *MidiOut) GetPortName(portNumber uint) string {
	// Check if the wrapper is valid
	if !out.ptr.ok {
		return fmt.Sprintf("Port %d (error)", portNumber)
	}

	// First call to get required buffer length
	var bufLen C.int = 0
	result := C.rtmidi_get_port_name(out.ptr, C.uint(portNumber), nil, &bufLen)
	if result != 0 || bufLen <= 0 {
		return fmt.Sprintf("Port %d (no name)", portNumber)
	}
	
	// Allocate buffer with extra space for null terminator
	buf := (*C.char)(C.malloc(C.size_t(bufLen + 1)))
	defer C.free(unsafe.Pointer(buf))
	
	// Zero the buffer
	for i := 0; i < int(bufLen+1); i++ {
		*(*C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(buf)) + uintptr(i))) = 0
	}
	
	result = C.rtmidi_get_port_name(out.ptr, C.uint(portNumber), buf, &bufLen)
	if result == 0 {
		name := C.GoString(buf)
		if name != "" {
			return name
		}
	}
	return fmt.Sprintf("Port %d", portNumber)
}

// SendMessage sends a MIDI message
func (out *MidiOut) SendMessage(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	C.rtmidi_out_send_message(out.ptr, (*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)))

	if !out.ptr.ok {
		return fmt.Errorf("failed to send MIDI message: %s", C.GoString(out.ptr.msg))
	}
	return nil
}

// Close closes the MIDI input port
func (in *MidiIn) Close() {
	if in.ptr != nil {
		midiCallbackRegistryMu.Lock()
		delete(midiCallbackRegistry, in.id)
		midiCallbackRegistryMu.Unlock()

		C.rtmidi_in_free(in.ptr)
		in.ptr = nil
		runtime.SetFinalizer(in, nil)
	}
}

// Close closes the MIDI output port
func (out *MidiOut) Close() {
	if out.ptr != nil {
		C.rtmidi_out_free(out.ptr)
		out.ptr = nil
		runtime.SetFinalizer(out, nil)
	}
}

// ListInputPorts returns a list of available MIDI input ports
func ListInputPorts() ([]string, error) {
	in, err := NewMidiIn()
	if err != nil {
		return nil, err
	}
	defer in.Close()

	count := in.GetPortCount()
	ports := make([]string, count)
	for i := uint(0); i < count; i++ {
		ports[i] = in.GetPortName(i)
	}
	return ports, nil
}

// ListOutputPorts returns a list of available MIDI output ports
func ListOutputPorts() ([]string, error) {
	out, err := NewMidiOut()
	if err != nil {
		return nil, err
	}
	defer out.Close()

	count := out.GetPortCount()
	ports := make([]string, count)
	for i := uint(0); i < count; i++ {
		ports[i] = out.GetPortName(i)
	}
	return ports, nil
}

//export rtmidi_callback
func rtmidi_callback(timestamp C.double, message *C.uchar, messageSize C.size_t, userData unsafe.Pointer) {
	id := uintptr(userData)
	midiCallbackRegistryMu.RLock()
	callback, exists := midiCallbackRegistry[id]
	midiCallbackRegistryMu.RUnlock()

	if exists {
		// Convert C array to Go slice
		data := C.GoBytes(unsafe.Pointer(message), C.int(messageSize))
		callback(data)
	}
}