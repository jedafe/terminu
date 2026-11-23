// Ref: .teos/conventions/env-vars-catalog.md, .teos/sections/01-foundation-tech-stack.md
// TU_* env vars; optional termiu.toml overrides; precedence env > file > default.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port               int
	Bind               string
	PublicURL          string
	DataDir            string
	SQLitePath         string
	SessionSecret      string
	AdminUser          string
	AdminPassHash      string
	AdminPassword      string
	AllowedOrigins     []string
	DefaultShell       string
	SessionBufferKB    int
	MaxSessions        int
	ReconnectWindowSec int
	QuiescenceMS       int
	ScanBottomRows     int
	LLMEnabled         bool
	LLMProvider        string
	LLMEndpoint        string
	LLMAPIKey          string
	LLMModel           string
	DockerSocket       string
	SFTPEnabled        bool
	SFTPPort           int
	LocalTargetMode    string // host | nsenter | container
	SSHKnownHosts      string
	LogLevel           string
	SecretsKey         string
	WSMaxFrameKB       int
	WebDevPort         int
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getint(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getbool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	return def
}

// Load reads termiu.toml (if present), then applies TU_* env over it.
// toml key = var minus TU_, lowercased, first '_' → '.' (env-vars-catalog).
func Load() (*Config, error) {
	c := &Config{
		Port:               7700,
		Bind:               "127.0.0.1",
		DataDir:            "./data",
		SessionBufferKB:    512,
		MaxSessions:        50,
		ReconnectWindowSec: 300,
		QuiescenceMS:       400,
		ScanBottomRows:     12,
		DockerSocket:       "/var/run/docker.sock",
		SFTPPort:           2022,
		LocalTargetMode:    "container",
		LogLevel:           "info",
		WSMaxFrameKB:       64,
		WebDevPort:         5173,
	}
	applyToml(c)
	c.Port = getint("TU_PORT", c.Port)
	c.Bind = getenv("TU_BIND", c.Bind)
	c.PublicURL = getenv("TU_PUBLIC_URL", c.PublicURL)
	c.DataDir = getenv("TU_DATA_DIR", c.DataDir)
	c.SQLitePath = getenv("TU_SQLITE_PATH", filepath.Join(c.DataDir, "terminu.db"))
	c.SessionSecret = getenv("TU_SESSION_SECRET", c.SessionSecret)
	c.AdminUser = getenv("TU_ADMIN_USER", c.AdminUser)
	c.AdminPassHash = getenv("TU_ADMIN_PASS_HASH", c.AdminPassHash)
	c.AdminPassword = getenv("TU_ADMIN_PASSWORD", c.AdminPassword)
	if v := strings.TrimSpace(os.Getenv("TU_ALLOWED_ORIGINS")); v != "" {
		c.AllowedOrigins = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
	}
	c.DefaultShell = getenv("TU_DEFAULT_SHELL", c.DefaultShell)
	c.SessionBufferKB = getint("TU_SESSION_BUFFER_KB", c.SessionBufferKB)
	c.MaxSessions = getint("TU_MAX_SESSIONS", c.MaxSessions)
	c.ReconnectWindowSec = getint("TU_RECONNECT_WINDOW_S", c.ReconnectWindowSec)
	c.QuiescenceMS = getint("TU_QUIESCENCE_MS", c.QuiescenceMS)
	c.ScanBottomRows = getint("TU_SCAN_BOTTOM_ROWS", c.ScanBottomRows)
	c.LLMEnabled = getbool("TU_LLM_ENABLED", c.LLMEnabled)
	c.LLMProvider = getenv("TU_LLM_PROVIDER", c.LLMProvider)
	c.LLMEndpoint = getenv("TU_LLM_ENDPOINT", c.LLMEndpoint)
	c.LLMAPIKey = getenv("TU_LLM_API_KEY", c.LLMAPIKey)
	c.LLMModel = getenv("TU_LLM_MODEL", c.LLMModel)
	c.DockerSocket = getenv("TU_DOCKER_SOCKET", c.DockerSocket)
	c.SFTPEnabled = getbool("TU_SFTP_ENABLED", c.SFTPEnabled)
	c.SFTPPort = getint("TU_SFTP_PORT", c.SFTPPort)
	c.LocalTargetMode = getenv("TU_LOCAL_TARGET_MODE", c.LocalTargetMode)
	c.SSHKnownHosts = getenv("TU_SSH_KNOWN_HOSTS", filepath.Join(c.DataDir, "known_hosts"))
	c.LogLevel = getenv("TU_LOG_LEVEL", c.LogLevel)
	c.SecretsKey = getenv("TU_SECRETS_KEY", c.SecretsKey)
	c.WSMaxFrameKB = getint("TU_WS_MAX_FRAME_KB", c.WSMaxFrameKB)
	c.WebDevPort = getint("TU_WEB_DEV_PORT", c.WebDevPort)

	switch c.LocalTargetMode {
	case "host", "nsenter", "container":
	default:
		return nil, fmt.Errorf("TU_LOCAL_TARGET_MODE must be host|nsenter|container, got %q", c.LocalTargetMode)
	}
	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return nil, err
	}
	return c, nil
}

// applyToml applies termiu.toml keys found in ./terminu.toml or $TU_DATA_DIR/terminu.toml.
// Minimal parser: [section] headers + key = "value" (env stays the primary surface).
func applyToml(c *Config) {
	for _, path := range []string{"./terminu.toml", "./data/terminu.toml"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		section := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.Trim(line, "[]")
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key := strings.TrimSpace(k)
			if section != "" {
				key = section + "." + key
			}
			val := strings.Trim(strings.TrimSpace(v), `"'`)
			c.setToml(key, val)
		}
	}
}

func (c *Config) setToml(key, val string) {
	switch key {
	case "port":
		c.Port, _ = strconv.Atoi(val)
	case "bind":
		c.Bind = val
	case "public.url":
		c.PublicURL = val
	case "data.dir":
		c.DataDir = val
	case "sqlite.path":
		c.SQLitePath = val
	case "session.secret":
		c.SessionSecret = val
	case "admin.user":
		c.AdminUser = val
	case "admin.pass_hash":
		c.AdminPassHash = val
	case "admin.password":
		c.AdminPassword = val
	case "allowed.origins":
		c.AllowedOrigins = strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' })
	case "default.shell":
		c.DefaultShell = val
	case "session.buffer_kb":
		c.SessionBufferKB, _ = strconv.Atoi(val)
	case "max.sessions":
		c.MaxSessions, _ = strconv.Atoi(val)
	case "reconnect.window_s":
		c.ReconnectWindowSec, _ = strconv.Atoi(val)
	case "quiescence.ms":
		c.QuiescenceMS, _ = strconv.Atoi(val)
	case "scan.bottom_rows":
		c.ScanBottomRows, _ = strconv.Atoi(val)
	case "llm.enabled":
		c.LLMEnabled = val == "true"
	case "docker.socket":
		c.DockerSocket = val
	case "sftp.enabled":
		c.SFTPEnabled = val == "true"
	case "sftp.port":
		c.SFTPPort, _ = strconv.Atoi(val)
	case "local.target_mode":
		c.LocalTargetMode = val
	case "ssh.known_hosts":
		c.SSHKnownHosts = val
	case "log.level":
		c.LogLevel = val
	case "secrets.key":
		c.SecretsKey = val
	case "ws.max_frame_kb":
		c.WSMaxFrameKB, _ = strconv.Atoi(val)
	case "web.dev_port":
		c.WebDevPort, _ = strconv.Atoi(val)
	}
}
