package utils

import (
	"fmt"
	"strconv"
	"time"
)

// MustParseTimeInterval returns a parsed duration from a string (as parsed by k8s clientcmd helpers) or dies
// A duration string value must be a positive integer, optionally followed by a corresponding time unit (s|m|h).
func MustParseTimeInterval(duration string) time.Duration {
	if i, err := strconv.ParseInt(duration, 10, 64); err == nil && i >= 0 {
		return (time.Duration(i) * time.Second)
	}
	if requestTimeout, err := time.ParseDuration(duration); err == nil {
		return requestTimeout
	}
	panic(fmt.Errorf("invalid timeout value. timeout must be a single integer in seconds, or an integer followed by a corresponding time unit (e.g. 1s | 2m | 3h)"))
}
