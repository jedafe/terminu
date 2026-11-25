// Ref: .teos/sections/06 — Pin() declares verified version coverage;
// a probed binary version outside the constraint degrades the badge to
// "unverified" (the adapter still runs — spec §9 fallback contract).
package adapter

import (
	"strconv"
	"strings"
)

// Pin status values — the adapter_pins table + /api/adapters surface.
const (
	PinVerified   = "verified"
	PinUnverified = "unverified"
	PinUnknown    = "unknown" // no binary version observed yet
)

// Satisfies reports whether version (e.g. "1.2.3") falls inside a pin
// constraint like ">=1.0 <2.0". An unparseable version is honest:
// it fails the constraint rather than guessing.
func Satisfies(constraint, version string) bool {
	v, ok := parseVersion(version)
	if !ok {
		return false
	}
	for _, term := range strings.Fields(constraint) {
		op, want := splitOp(term)
		w, ok := parseVersion(want)
		if !ok {
			continue // a bad constraint term can't gate anything
		}
		cmp := compareVersion(v, w)
		switch op {
		case ">=":
			if cmp < 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case "<":
			if cmp >= 0 {
				return false
			}
		case "=", "==":
			if cmp != 0 {
				return false
			}
		case "!=":
			if cmp == 0 {
				return false
			}
		}
	}
	return true
}

// PinStatus evaluates a seen version against the constraint — the badge
// state for a bound adapter.
func PinStatus(constraint, seenVersion string) string {
	if seenVersion == "" {
		return PinUnknown
	}
	if Satisfies(constraint, seenVersion) {
		return PinVerified
	}
	return PinUnverified
}

// parseVersion reads X[.Y[.Z]] — a leading v and suffixes (-beta.1) are
// tolerated; components are numeric only.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimSpace(strings.TrimPrefix(s, "v"))
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersion(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return 0
}

// splitOp separates a leading comparison operator from its version.
func splitOp(term string) (op, ver string) {
	for i, r := range term {
		if r >= '0' && r <= '9' {
			return term[:i], term[i:]
		}
	}
	return "", term
}
