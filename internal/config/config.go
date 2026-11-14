package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"lcs/internal/identity"
)

// Config holds the runtime configuration resolved from the environment.
//
// Environment variables:
//
//	LCS_WORKSPACE  shared folder (NAS) that holds projects/*/repo.git   [required]
//	LCS_DATA_DIR   local per-machine state (db, local clones, cache)
//	LCS_PORT       127.0.0.1 listen port (default 7809)
//	LCS_ACTOR      display name recorded on commits (default OS user)
//	LCS_EMAIL      identity override; by default it is the Windows UPN (whoami /upn)
//	LCS_SEED_DIR   optional folder imported into a new project
//
// A JSON file at <LCS_DATA_DIR>/config.json is used as a fallback so the exe
// works on double click without depending on the environment of Explorer:
//
//	{"workspace":"D:\\LCS","port":7809,"actor":"danie"}
type Config struct {
	Workspace string
	DataDir   string
	Port      int
	Actor     string
	SeedDir   string
	// Source says where the workspace came from: "entorno" or "config.json".
	Source string

	// Identity of the person using this machine. It comes from the Windows
	// session (whoami /upn) unless LCS_EMAIL / config.json "email" overrides it.
	Email         string
	EmailVerified bool
	EmailSource   string
}

type fileConfig struct {
	Workspace string `json:"workspace"`
	Port      int    `json:"port"`
	Actor     string `json:"actor"`
	SeedDir   string `json:"seedDir"`
	Email     string `json:"email"`
}

// ConfigPath is the on-disk configuration file read when env vars are absent.
func ConfigPath(dataDir string) string { return filepath.Join(dataDir, "config.json") }

func readFileConfig(dataDir string) (fileConfig, error) {
	var fc fileConfig
	raw, err := os.ReadFile(ConfigPath(dataDir))
	if err != nil {
		return fc, err
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return fc, fmt.Errorf("config.json inválido: %w", err)
	}
	return fc, nil
}

func Load() (*Config, error) {
	c := &Config{}

	// DataDir first: the config file lives inside it.
	c.DataDir = strings.TrimSpace(os.Getenv("LCS_DATA_DIR"))
	if c.DataDir == "" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.TempDir()
		}
		c.DataDir = filepath.Join(base, "LCS")
	}
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("LCS_DATA_DIR inválido: %w", err)
	}
	c.DataDir = abs

	fc, ferr := readFileConfig(c.DataDir)
	if ferr != nil && !errors.Is(ferr, os.ErrNotExist) {
		return nil, ferr
	}

	c.Workspace = strings.TrimSpace(os.Getenv("LCS_WORKSPACE"))
	if c.Workspace != "" {
		c.Source = "entorno"
	} else if fc.Workspace != "" {
		c.Workspace = fc.Workspace
		c.Source = "config.json"
	}

	c.Actor = strings.TrimSpace(os.Getenv("LCS_ACTOR"))
	if c.Actor == "" {
		c.Actor = strings.TrimSpace(fc.Actor)
	}
	c.SeedDir = strings.TrimSpace(os.Getenv("LCS_SEED_DIR"))
	if c.SeedDir == "" {
		c.SeedDir = strings.TrimSpace(fc.SeedDir)
	}

	port := strings.TrimSpace(os.Getenv("LCS_PORT"))
	if port == "" && fc.Port > 0 {
		port = strconv.Itoa(fc.Port)
	}
	if port == "" {
		port = "7809"
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return nil, fmt.Errorf("LCS_PORT inválido: %q", port)
	}
	c.Port = p

	if c.Workspace == "" {
		return nil, fmt.Errorf(
			"falta el workspace: definí LCS_WORKSPACE o creá %s con {\"workspace\":\"D:\\\\LCS\"}",
			ConfigPath(c.DataDir),
		)
	}
	abs, err = filepath.Abs(c.Workspace)
	if err != nil {
		return nil, fmt.Errorf("workspace inválido: %w", err)
	}
	c.Workspace = abs

	if c.Actor == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			name := u.Username
			if i := strings.LastIndexAny(name, `\/`); i >= 0 {
				name = name[i+1:]
			}
			c.Actor = name
		} else {
			c.Actor = "operador"
		}
	}

	emailOverride := strings.TrimSpace(os.Getenv("LCS_EMAIL"))
	if emailOverride == "" {
		emailOverride = strings.TrimSpace(fc.Email)
	}
	if emailOverride != "" && !identity.IsEmail(emailOverride) {
		return nil, fmt.Errorf("LCS_EMAIL inválido: %q no es un correo", emailOverride)
	}
	id := identity.Resolve(emailOverride)
	c.Email, c.EmailVerified, c.EmailSource = id.Email, id.Verified, id.Source

	return c, nil
}

func (c *Config) ProjectsDir() string  { return filepath.Join(c.Workspace, "projects") }
func (c *Config) LocksDir() string     { return filepath.Join(c.Workspace, ".lcs", "locks") }
func (c *Config) DBPath() string       { return filepath.Join(c.DataDir, "lcs.db") }
func (c *Config) ClonesDir() string    { return filepath.Join(c.DataDir, "repos") }
func (c *Config) CacheDir() string     { return filepath.Join(c.DataDir, "cache") }
func (c *Config) DownloadsDir() string { return filepath.Join(c.DataDir, "downloads") }
func (c *Config) LocalURL() string     { return fmt.Sprintf("http://127.0.0.1:%d/", c.Port) }

// EnsureDirs creates the local and shared directory skeleton.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.Workspace,
		c.ProjectsDir(),
		c.LocksDir(),
		c.DataDir,
		c.ClonesDir(),
		c.CacheDir(),
		c.DownloadsDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("no se pudo crear %s: %w", d, err)
		}
	}
	return nil
}
