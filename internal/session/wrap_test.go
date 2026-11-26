package session

import (
	"reflect"
	"testing"
)

func TestWrapArgv(t *testing.T) {
	cases := []struct {
		name string
		wrap string
		argv []string
		want []string
	}{
		{"no wrap keeps argv", "", []string{"claude", "--continue"}, []string{"claude", "--continue"}},
		{"wrap shells out", "/bin/zsh", []string{"claude", "--continue"}, []string{"/bin/zsh", "-lc", "'claude' '--continue'"}},
		{"wrap quotes spaces", "/bin/bash", []string{"devin", "--msg", "a b"}, []string{"/bin/bash", "-lc", "'devin' '--msg' 'a b'"}},
		{"wrap ignored without argv", "/bin/zsh", nil, nil},
	}
	for _, tc := range cases {
		if got := wrapArgv(tc.wrap, tc.argv); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
