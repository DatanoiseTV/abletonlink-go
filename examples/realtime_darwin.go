//go:build darwin

package main

/*
#include <mach/mach.h>
#include <mach/thread_policy.h>
#include <mach/thread_act.h>

int set_realtime_priority() {
    thread_time_constraint_policy_data_t policy;
    thread_port_t thread_port = mach_thread_self();
    
    // Set up time constraint policy for real-time scheduling
    // These values are typical for audio applications
    policy.period = 128 * 1000;      // 128 microseconds (about 8kHz)
    policy.computation = 64 * 1000;   // 64 microseconds max computation time
    policy.constraint = 96 * 1000;    // 96 microseconds constraint
    policy.preemptible = 1;           // Allow preemption
    
    kern_return_t result = thread_policy_set(
        thread_port,
        THREAD_TIME_CONSTRAINT_POLICY,
        (thread_policy_t)&policy,
        THREAD_TIME_CONSTRAINT_POLICY_COUNT
    );
    
    return result == KERN_SUCCESS ? 0 : -1;
}
*/
import "C"
import "fmt"

// setRealtimePriority sets real-time priority on macOS using Mach time constraint policy
func setRealtimePriority() error {
	result := C.set_realtime_priority()
	if result != 0 {
		return fmt.Errorf("failed to set real-time priority on macOS (code: %d)", result)
	}
	return nil
}