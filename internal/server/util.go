package server

import (
	"strconv"
	"time"
)

const Version = "0.1.0-phase1"

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func parseT(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
