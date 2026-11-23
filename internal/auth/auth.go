// Ref: .teos/sections/09-authn-authz-broker.md
// Cookie sessions over argon2id — single-admin bootstrap via TU_ADMIN_*.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db/gen"
)

const CookieName = "terminu_session"
const sessionTTL = 30 * 24 * time.Hour

// argon2id params (OWASP baseline).
const (
	aTime    = 1
	aMemory  = 64 * 1024
	aThreads = 4
	aKeyLen  = 32
	saltLen  = 16
)

type Service struct {
	q *gen.Queries
}

type Principal struct {
	UserID   int64
	Username string
	Role     string
}

func New(q *gen.Queries) *Service { return &Service{q: q} }

// HashPassword returns the $argon2id$ PHC string for TU_ADMIN_PASS_HASH.
func HashPassword(pw string) string {
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, aTime, aMemory, aThreads, aKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, aMemory, aTime, aThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

func verify(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		return false
	}
	var mem, t, p uint32
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Seed creates the first admin when users is empty. TU_ADMIN_PASS_HASH
// preferred; TU_ADMIN_PASSWORD is hashed then discarded (never stored).
func (s *Service) Seed(ctx context.Context, cfg *config.Config) error {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.AdminUser == "" {
		return nil // no seed requested — first-run locked
	}
	hash := cfg.AdminPassHash
	if hash == "" && cfg.AdminPassword != "" {
		hash = HashPassword(cfg.AdminPassword)
	}
	if hash == "" {
		return errors.New("TU_ADMIN_USER set but no TU_ADMIN_PASS_HASH/TU_ADMIN_PASSWORD")
	}
	res, err := s.q.CreateUser(ctx, gen.CreateUserParams{
		Username: cfg.AdminUser, PasswordHash: hash, Role: "admin"})
	if err != nil {
		return err
	}
	_ = res
	return nil
}

// Login checks credentials and returns a session token.
func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	u, err := s.q.GetUserByName(ctx, username)
	if err != nil {
		return "", errors.New("invalid credentials")
	}
	if !verify(u.PasswordHash, password) {
		return "", errors.New("invalid credentials")
	}
	tok := make([]byte, 32)
	_, _ = rand.Read(tok)
	token := base64.RawURLEncoding.EncodeToString(tok)
	sum := sha256.Sum256([]byte(token))
	exp := time.Now().UTC().Add(sessionTTL).Format(time.RFC3339)
	if _, err := s.q.CreateAuthSession(ctx, gen.CreateAuthSessionParams{
		UserID: u.ID, TokenHash: hex.EncodeToString(sum[:]), ExpiresAt: exp}); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) Logout(ctx context.Context, token string) {
	sum := sha256.Sum256([]byte(token))
	_ = s.q.RevokeAuthSession(ctx, hex.EncodeToString(sum[:]))
}

// Authenticate resolves the cookie to a principal.
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (*Principal, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, errors.New("unauthorized")
	}
	sum := sha256.Sum256([]byte(c.Value))
	row, err := s.q.GetAuthSession(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, errors.New("unauthorized")
	}
	if row.RevokedAt.Valid {
		return nil, errors.New("unauthorized")
	}
	exp, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil || time.Now().After(exp) {
		return nil, errors.New("unauthorized")
	}
	return &Principal{UserID: row.UserID, Username: row.Username, Role: row.Role}, nil
}

// SetCookie writes the session cookie.
func SetCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
	})
}

var _ = sql.ErrNoRows
