//go:build darwin

package main

import "github.com/ebitengine/purego"

var mallocZonePressureRelief func(zone uintptr, goal uintptr) uintptr

func init() {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err == nil {
		purego.RegisterLibFunc(&mallocZonePressureRelief, lib, "malloc_zone_pressure_relief")
	}
}

// releaseNativeMemory returns the pages malloc holds free for reuse to the
// system: after the panel closes, a third of malloc's resident memory was
// such free pages. Zone 0 is every zone; goal 0 is as much as it can.
func releaseNativeMemory() {
	if mallocZonePressureRelief != nil {
		mallocZonePressureRelief(0, 0)
	}
}
