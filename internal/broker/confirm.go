// Ref: .teos/sections/09 — confirmation step of the broker pipeline:
// a short-TTL token bound to the exact argv so it can't be replayed
// against a different command. Stateless HMAC — no table needed; the
// daemon's SessionSecret is the signing key.
package broker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ConfirmTTL is the token's validity window — long enough to read the
// confirm dialog, short enough that a leaked token is useless.
const ConfirmTTL = 60 * time.Second

// MintConfirmation signs (expiry, actor, action, argv) — the client's
// confirm dialog echoes this token back to prove the human approved
// THIS argv, not an op in general.
func MintConfirmation(secret, actor, action string, argv []string) string {
	exp := time.Now().Add(ConfirmTTL).Unix()
	return mint(secret, actor, action, argv, exp)
}

func mint(secret, actor, action string, argv []string, exp int64) string {
	argvJSON, _ := json.Marshal(argv)
	payload := strconv.FormatInt(exp, 10) + "|" + actor + "|" + action + "|" + string(argvJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(exp, 10))) + "." +
		hex.EncodeToString(mac.Sum(nil))
}

// VerifyConfirmation checks signature, expiry, and binding. A token
// minted for argv A never verifies for argv B (the argv is inside the
// HMAC payload).
func VerifyConfirmation(token, secret, actor, action string, argv []string) bool {
	expB64, sigHex, ok := strings.Cut(token, ".")
	if !ok || sigHex == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(expB64)
	if err != nil {
		return false
	}
	exp, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(mint(secret, actor, action, argv, exp)), []byte(token))
}
