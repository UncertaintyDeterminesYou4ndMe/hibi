package main

import "time"

const (
	// activityCheckAfter without input, Hibi asks the system whether a video
	// or a call goes on: before that the input says the user is here.
	activityCheckAfter = 30 * time.Second
	// typingPause without a key press is a pause in typing, where the focus
	// end prompt can come.
	typingPause = 8 * time.Second
	// typingDeferLimit is the longest the prompt waits for that pause;
	// callDeferLimit the longest it waits for a call to end.
	typingDeferLimit = 3 * time.Minute
	callDeferLimit   = time.Hour
	// deferCheckInterval is how often a deferred prompt looks again.
	deferCheckInterval = 2 * time.Second
)

// busyReason is why now is a bad moment for a prompt.
type busyReason int

const (
	notBusy busyReason = iota
	busyTyping
	busyCall
)

// deferLimit is how long a prompt may wait for the reason to end.
func (r busyReason) deferLimit() time.Duration {
	switch r {
	case busyTyping:
		return typingDeferLimit
	case busyCall:
		return callDeferLimit
	}
	return 0
}

// currentBusy reads what the user is doing from the system: in a call
// (the microphone is in use), or typing (a key press within typingPause).
func currentBusy() busyReason {
	if microphoneInUse() {
		return busyCall
	}
	if sinceKeyPress() < typingPause {
		return busyTyping
	}
	return notBusy
}

// watchingOrInCall reports whether the user is at the computer without
// input: a video plays or a call goes on.
func watchingOrInCall() bool {
	if held, _ := displayHeldAwake(); held {
		return true
	}
	return microphoneInUse()
}

// held is the idle time with presence held while a video plays or a call
// goes on: after activityCheckAfter without input it asks watching, and a
// yes holds the user present for deskMotionHold. aware off asks nothing.
func (t *presenceHold) held(system time.Duration, aware bool, watching func() bool) time.Duration {
	if aware && system >= activityCheckAfter && watching() {
		t.report()
	}
	return t.idle(system)
}
