//go:build linux

package server

import (
	"reflect"
	"testing"
)

func sp(s string) *string      { return &s }
func sa(s ...string) *[]string { return &s }

func TestValidateProfileShellVariants(t *testing.T) {
	// direct exec stays required-argv
	if _, _, _, _, _, _, _, err := validateProfile(profileIn{Name: sp("x"), Argv: sa()}); err == nil {
		t.Fatal("expected argv-required error")
	}
	// shell-only is valid without argv
	name, argv, _, _, _, shell, _, err := validateProfile(profileIn{
		Name: sp("work"), Shell: sp("/bin/zsh"), Argv: sa(),
	})
	if err != nil || argv != nil || name != "work" || shell != "/bin/zsh" {
		t.Fatalf("shell-only: %v %v %q %q", err, argv, name, shell)
	}
	// variants validate
	_, _, _, _, _, _, vj, err := validateProfile(profileIn{
		Name: sp("devin"), Argv: sa("devin"),
		Variants: &[]profileVariant{
			{Name: "cloud", Argv: []string{"devin", "--cloud"}},
			{Name: "continue", Argv: []string{"devin", "--continue"}},
		},
	})
	if err != nil || vj == "" {
		t.Fatalf("variants: %v %q", err, vj)
	}
	// bad shell rejected
	if _, _, _, _, _, _, _, err = validateProfile(profileIn{
		Name: sp("x"), Shell: sp("bad shell!"), Argv: sa("x"),
	}); err == nil {
		t.Fatal("expected shell validation error")
	}
	// variant without argv[0] rejected
	if _, _, _, _, _, _, _, err = validateProfile(profileIn{
		Name: sp("x"), Argv: sa("x"),
		Variants: &[]profileVariant{{Name: "bad", Argv: []string{}}},
	}); err == nil {
		t.Fatal("expected variant argv error")
	}
}

func TestResolveLaunchArgv(t *testing.T) {
	prof := profileOut{
		Argv:      []string{"devin"},
		ResumeCmd: []string{"devin", "--resume"},
		Shell:     "/bin/zsh",
		Variants:  []profileVariant{{Name: "cloud", Argv: []string{"devin", "--cloud"}}},
	}
	argv, wrap, err := resolveLaunchArgv(prof, "", false)
	if err != nil || !reflect.DeepEqual(argv, []string{"devin"}) || wrap != "/bin/zsh" {
		t.Fatalf("default: %v %q %v", argv, wrap, err)
	}
	argv, _, err = resolveLaunchArgv(prof, "cloud", false)
	if err != nil || !reflect.DeepEqual(argv, []string{"devin", "--cloud"}) {
		t.Fatalf("variant: %v %v", argv, err)
	}
	if _, _, err = resolveLaunchArgv(prof, "nope", false); err == nil {
		t.Fatal("expected variant_not_found")
	}
	argv, _, _ = resolveLaunchArgv(prof, "", true)
	if !reflect.DeepEqual(argv, []string{"devin", "--resume"}) {
		t.Fatalf("resume: %v", argv)
	}
	// shell-only profile: empty argv stays empty, Shell goes to Shell
	sh := profileOut{Shell: "/bin/fish"}
	argv, wrap, err = resolveLaunchArgv(sh, "", false)
	if err != nil || argv != nil || wrap != "/bin/fish" {
		t.Fatalf("shell-only: %v %q %v", argv, wrap, err)
	}
}

func TestToolOf(t *testing.T) {
	if got := toolOf(profileOut{Argv: []string{"/usr/bin/claude", "-p"}}); got != "claude" {
		t.Fatalf("argv basename: %q", got)
	}
	if got := toolOf(profileOut{
		Argv:     []string{},
		Variants: []profileVariant{{Name: "c", Argv: []string{"/opt/devin", "--cloud"}}},
	}); got != "devin" {
		t.Fatalf("variant fallback: %q", got)
	}
	if got := toolOf(profileOut{}); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
