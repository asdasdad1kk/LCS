package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv reads a .env file beside the executable, falling back to the
// current directory. Existing process environment values always take priority.
func LoadDotEnv() error {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ".env"))
	}

	seen := make(map[string]bool)
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("no se pudo leer %s: %w", path, err)
		}
		scan := bufio.NewScanner(f)
		lineNo := 0
		for scan.Scan() {
			lineNo++
			line := strings.TrimSpace(scan.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, value, ok := strings.Cut(line, "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				f.Close()
				return fmt.Errorf("línea %d inválida en %s: se esperaba CLAVE=valor", lineNo, path)
			}
			value = strings.TrimSpace(value)
			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}
			if current, exists := os.LookupEnv(key); !exists || strings.TrimSpace(current) == "" {
				if err := os.Setenv(key, value); err != nil {
					f.Close()
					return fmt.Errorf("variable %s inválida en %s: %w", key, path, err)
				}
			}
		}
		scanErr := scan.Err()
		closeErr := f.Close()
		if scanErr != nil {
			return fmt.Errorf("no se pudo leer %s: %w", path, scanErr)
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	}
	return nil
}
