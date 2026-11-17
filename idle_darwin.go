//go:build darwin

package main

import (
	"time"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

var cgSecondsSinceLastEvent func(state int32, eventType uint32) float64

func init() {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err == nil {
		purego.RegisterLibFunc(&cgSecondsSinceLastEvent, lib, "CGEventSourceSecondsSinceLastEventType")
	}
}

// systemIdle is the time since the last keyboard or mouse input. It asks
// Core Graphics on the calling goroutine: mygo.Power.IdleTime makes the same
// call on the main thread, and the hop there woke the process about a dozen
// times a read.
func systemIdle() time.Duration {
	if cgSecondsSinceLastEvent == nil {
		return mygo.Power.IdleTime()
	}
	const combinedSessionState, anyInputEvent = 0, ^uint32(0)
	return time.Duration(cgSecondsSinceLastEvent(combinedSessionState, anyInputEvent) * float64(time.Second))
}
