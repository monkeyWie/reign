//go:build (darwin && !ios) || (linux && !android)

package terminal

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const SupportsProfiles = true

func Profiles(ctx context.Context) ([]Profile, error) {
	profiles := []Profile{}
	seen := map[string]bool{}
	add := func(path string) {
		name := filepath.Base(path)
		if !filepath.IsAbs(path) || seen[name] || name == "false" || name == "nologin" {
			return
		}
		if _, err := exec.LookPath(path); err != nil {
			return
		}
		seen[name] = true
		profiles = append(profiles, Profile{ID: fmt.Sprintf("shell-%x", sha256.Sum256([]byte(path))), Name: name, path: path})
	}
	add(os.Getenv("SHELL"))
	// Registered login shells may live outside the GUI application's PATH.
	file, err := os.Open("/etc/shells")
	if err == nil {
		defer file.Close()
		scan := bufio.NewScanner(file)
		for scan.Scan() && ctx.Err() == nil {
			line, _, _ := strings.Cut(scan.Text(), "#")
			add(strings.TrimSpace(line))
		}
	}
	for _, name := range []string{"sh", "bash", "zsh", "fish", "nu", "pwsh"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if path, err := exec.LookPath(name); err == nil {
			add(path)
		}
	}
	return profiles, nil
}
