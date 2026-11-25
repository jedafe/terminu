package session

import (
	"slices"
	"testing"

	"github.com/jedafe/terminu/internal/shellenv"
)

func TestShellArgvManaged(t *testing.T) {
	m := &Manager{env: shellenv.Env{
		ZshDir:   "/data/zsh",
		FishFile: "/data/fish/terminu.fish",
		BashFile: "/data/bash/terminu.bash",
	}}
	cases := []struct {
		shell string
		want  []string
	}{
		{"/usr/bin/zsh", []string{"env", "ZDOTDIR=/data/zsh", "/usr/bin/zsh", "-l"}},
		{"/usr/bin/fish", []string{"/usr/bin/fish", "-l", "-C", "source /data/fish/terminu.fish"}},
		{"/usr/bin/bash", []string{"/usr/bin/bash", "--rcfile", "/data/bash/terminu.bash", "-l"}},
		{"/bin/sh", []string{"/bin/sh", "-l"}}, // unknown shells stay plain
	}
	for _, c := range cases {
		if got := m.shellArgv(c.shell); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v want %v", c.shell, got, c.want)
		}
	}
}

func TestShellArgvDegraded(t *testing.T) {
	m := &Manager{} // materialization failed → zero Env → plain spawns
	for _, sh := range []string{"/bin/zsh", "/usr/bin/fish", "/bin/bash"} {
		want := []string{sh, "-l"}
		if got := m.shellArgv(sh); !slices.Equal(got, want) {
			t.Errorf("%s: got %v want %v", sh, got, want)
		}
	}
}
