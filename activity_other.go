//go:build !darwin

package main

import "time"

// Other systems do not report these yet: nothing holds presence or defers
// a prompt.
func displayHeldAwake() (bool, string) { return false, "" }
func microphoneInUse() bool            { return false }
func sinceKeyPress() time.Duration     { return time.Hour }
