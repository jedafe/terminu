// Ref: .teos/sections/06 — pin constraint checks: a probed version
// inside the range reads verified, outside reads unverified, and the
// badge stays unknown until a version is observed.
package adapter

import "testing"

func TestPinStatus(t *testing.T) {
	for _, c := range []struct {
		constraint, seen, want string
	}{
		{">=1.0 <2.0", "1.4.2", PinVerified},
		{">=1.0 <2.0", "2.0.0", PinUnverified},
		{">=1.0 <2.0", "0.9.9", PinUnverified},
		{">=0.70 <1.0", "0.70.1", PinVerified},
		{">=0.70 <1.0", "", PinUnknown},
		{">=1.0 <2.0", "garbage", PinUnverified},
		// No declared constraint → a seen version can't violate it.
		{"", "1.0.0", PinVerified},
		{"", "", PinUnknown},
	} {
		if got := PinStatus(c.constraint, c.seen); got != c.want {
			t.Errorf("PinStatus(%q,%q) = %q want %q", c.constraint, c.seen, got, c.want)
		}
	}
}

func TestSatisfies(t *testing.T) {
	for _, c := range []struct {
		constraint, version string
		want                bool
	}{
		{">=0.1 <1.0", "0.40.0", true},
		{">=0.1 <1.0", "1.0.0", false},
		{">=1.0 <2.0", "v1.4.2-beta.1", true},
		{"=1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		{"!=1.0.0", "1.0.0", false},
		{">=0.70", "0.70", true},
		{">=0.70", "nonsense", false},
	} {
		if got := Satisfies(c.constraint, c.version); got != c.want {
			t.Errorf("Satisfies(%q,%q) = %v want %v", c.constraint, c.version, got, c.want)
		}
	}
}
