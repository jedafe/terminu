// Ref: .teos/sections/09 — confirmation tokens: short-TTL, bound to the
// exact argv, HMAC-signed, never replayable against another command.
package broker

import "testing"

func TestConfirmationRoundtrip(t *testing.T) {
	argv := []string{"docker", "system", "prune", "-f"}
	tok := MintConfirmation("secret", "admin", "command.execute", argv)
	if !VerifyConfirmation(tok, "secret", "admin", "command.execute", argv) {
		t.Fatal("valid token rejected")
	}
}

func TestConfirmationBindsArgv(t *testing.T) {
	tok := MintConfirmation("secret", "admin", "command.execute", []string{"rm", "a"})
	if VerifyConfirmation(tok, "secret", "admin", "command.execute", []string{"rm", "b"}) {
		t.Fatal("token replayed against different argv")
	}
	if VerifyConfirmation(tok, "secret", "root", "command.execute", []string{"rm", "a"}) {
		t.Fatal("token replayed by different actor")
	}
	if VerifyConfirmation(tok, "secret", "admin", "agent.launch", []string{"rm", "a"}) {
		t.Fatal("token replayed for different action")
	}
	if VerifyConfirmation(tok, "other-secret", "admin", "command.execute", []string{"rm", "a"}) {
		t.Fatal("token verified under a different secret")
	}
	if VerifyConfirmation("bogus", "secret", "admin", "command.execute", []string{"rm", "a"}) {
		t.Fatal("malformed token verified")
	}
}

func TestConfirmationExpiry(t *testing.T) {
	// A token minted for the past must not verify — mint with an
	// already-dead expiry directly.
	tok := mint("secret", "admin", "command.execute", []string{"x"}, 1)
	if VerifyConfirmation(tok, "secret", "admin", "command.execute", []string{"x"}) {
		t.Fatal("expired token verified")
	}
}
