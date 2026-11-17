//go:build !darwin

package main

import (
	"time"

	"github.com/egoist/mygo"
)

// systemIdle is the time since the last keyboard or mouse input.
func systemIdle() time.Duration { return mygo.Power.IdleTime() }
