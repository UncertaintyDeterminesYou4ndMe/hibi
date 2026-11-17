//go:build darwin

package main

import (
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The system state that says what the user is doing without input: a video
// or a call keeps the display awake, and a call uses the microphone. Hibi
// reads only these flags and the time since the last key press: no screen
// contents, window titles or audio.
var (
	cfDictionaryGetCount          func(d uintptr) int
	cfDictionaryGetKeysAndValues  func(d uintptr, keys, values *uintptr)
	cfDictionaryGetValue          func(d, key uintptr) uintptr
	cfArrayGetCount               func(a uintptr) int
	cfArrayGetValueAtIndex        func(a uintptr, i int) uintptr
	cfStringCreateWithCString     func(alloc uintptr, s string, encoding uint32) uintptr
	cfStringGetCString            func(s uintptr, buf *byte, size int, encoding uint32) bool
	cfNumberGetValue              func(n uintptr, kind int, out *int32) bool
	cfGetTypeID                   func(v uintptr) uintptr
	cfStringGetTypeID             func() uintptr
	cfRelease                     func(v uintptr)
	ioPMCopyAssertionsByProcess   func(out *uintptr) int32
	audioObjectGetPropertyData    func(object uint32, address *audioAddress, qualifierSize uint32, qualifier unsafe.Pointer, size *uint32, data unsafe.Pointer) int32
	keyAssertType, keyAssertLevel uintptr
	keyProcessName                uintptr
)

const cfStringEncodingUTF8 = 0x08000100

type audioAddress struct{ selector, scope, element uint32 }

func fourCC(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

func init() {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.RegisterLibFunc(&cfDictionaryGetCount, cf, "CFDictionaryGetCount")
	purego.RegisterLibFunc(&cfDictionaryGetKeysAndValues, cf, "CFDictionaryGetKeysAndValues")
	purego.RegisterLibFunc(&cfDictionaryGetValue, cf, "CFDictionaryGetValue")
	purego.RegisterLibFunc(&cfArrayGetCount, cf, "CFArrayGetCount")
	purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
	purego.RegisterLibFunc(&cfNumberGetValue, cf, "CFNumberGetValue")
	purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
	purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	keyAssertType = cfStringCreateWithCString(0, "AssertType", cfStringEncodingUTF8)
	keyAssertLevel = cfStringCreateWithCString(0, "AssertLevel", cfStringEncodingUTF8)
	keyProcessName = cfStringCreateWithCString(0, "Process Name", cfStringEncodingUTF8)

	if iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err == nil {
		purego.RegisterLibFunc(&ioPMCopyAssertionsByProcess, iokit, "IOPMCopyAssertionsByProcess")
	}
	if audio, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_GLOBAL|purego.RTLD_NOW); err == nil {
		purego.RegisterLibFunc(&audioObjectGetPropertyData, audio, "AudioObjectGetPropertyData")
	}
}

func cfString(v uintptr) string {
	if v == 0 || cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	var buf [256]byte
	if !cfStringGetCString(v, &buf[0], len(buf), cfStringEncodingUTF8) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return ""
}

// keepAwakeTools hold the display awake for the user's own reasons, not
// because something plays.
var keepAwakeTools = map[string]bool{
	"caffeinate": true, "Amphetamine": true, "KeepingYouAwake": true, "Lungo": true, "Theine": true, "Hibi": true,
}

// displayHeldAwake reports whether a process keeps the display from sleeping
// while the user does not touch the computer, as video players, browsers
// playing a video and call apps do, and names it.
func displayHeldAwake() (bool, string) {
	if ioPMCopyAssertionsByProcess == nil || cfRelease == nil {
		return false, ""
	}
	var byProcess uintptr
	if ioPMCopyAssertionsByProcess(&byProcess) != 0 || byProcess == 0 {
		return false, ""
	}
	defer cfRelease(byProcess)
	n := cfDictionaryGetCount(byProcess)
	if n == 0 {
		return false, ""
	}
	keys, values := make([]uintptr, n), make([]uintptr, n)
	cfDictionaryGetKeysAndValues(byProcess, &keys[0], &values[0])
	for _, list := range values {
		for i := range cfArrayGetCount(list) {
			assertion := cfArrayGetValueAtIndex(list, i)
			if cfString(cfDictionaryGetValue(assertion, keyAssertType)) != "PreventUserIdleDisplaySleep" {
				continue
			}
			var level int32
			if v := cfDictionaryGetValue(assertion, keyAssertLevel); v != 0 && cfNumberGetValue(v, 3, &level) && level == 0 {
				continue
			}
			name := cfString(cfDictionaryGetValue(assertion, keyProcessName))
			if !keepAwakeTools[name] {
				return true, name
			}
		}
	}
	return false, ""
}

// microphoneInUse reports whether some app records from the default input
// device, as a call does (also while muted, for most call apps).
func microphoneInUse() bool {
	if audioObjectGetPropertyData == nil {
		return false
	}
	const systemObject = 1
	glob := fourCC("glob")
	var device uint32
	size := uint32(unsafe.Sizeof(device))
	addr := audioAddress{fourCC("dIn "), glob, 0}
	if audioObjectGetPropertyData(systemObject, &addr, 0, nil, &size, unsafe.Pointer(&device)) != 0 || device == 0 {
		return false
	}
	var running uint32
	size = uint32(unsafe.Sizeof(running))
	addr = audioAddress{fourCC("gone"), glob, 0}
	if audioObjectGetPropertyData(device, &addr, 0, nil, &size, unsafe.Pointer(&running)) != 0 {
		return false
	}
	return running != 0
}

// sinceKeyPress is the time since the last key press.
func sinceKeyPress() time.Duration {
	if cgSecondsSinceLastEvent == nil {
		return time.Hour
	}
	const combinedSessionState, keyDown = 0, 10
	return time.Duration(cgSecondsSinceLastEvent(combinedSessionState, keyDown) * float64(time.Second))
}
