package system

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
)

const DesktopSupported = true
const DesktopUnsupportedReason = ""

// Only installation locations are cached. Executables and running processes
// are revalidated on every probe, including after an App update or uninstall.
var codexPackages struct {
	sync.Mutex
	at    time.Time
	roots []string
	err   error
}

func FindApp(configured string) string {
	app, _ := FindAppContext(context.Background(), configured)
	return app
}

func FindAppContext(ctx context.Context, configured string) (string, error) {
	if configured != "" && AppExecutable(configured) != "" {
		return filepath.Abs(configured)
	}
	roots, err := installedCodexPackages(ctx)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if AppExecutable(root) != "" {
			return root, nil
		}
	}
	return "", nil
}

func installedCodexPackages(ctx context.Context) ([]string, error) {
	codexPackages.Lock()
	defer codexPackages.Unlock()
	ttl := 15 * time.Second
	if len(codexPackages.roots) == 0 || codexPackages.err != nil {
		ttl = 2 * time.Second
	}
	if !codexPackages.at.IsZero() && time.Since(codexPackages.at) < ttl {
		return codexPackages.roots, codexPackages.err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	// The exact package name avoids matching ChatGPT or unrelated executables.
	// No user input is interpolated into the PowerShell program.
	script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); ConvertTo-Json -Compress -InputObject @(Get-AppxPackage -Name OpenAI.Codex | Select-Object -ExpandProperty InstallLocation)`
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	var roots []string
	if err == nil {
		err = json.Unmarshal(out, &roots)
	}
	if err != nil {
		err = fmt.Errorf("查询 Codex Windows 应用包失败：%w", err)
	}
	codexPackages.roots, codexPackages.err, codexPackages.at = roots, err, time.Now()
	return roots, err
}

// app may be a package root, an unpacked application directory or an explicit
// executable override. The package manifest is authoritative when present.
func AppExecutable(app string) string {
	if app == "" {
		return ""
	}
	app, err := filepath.Abs(app)
	if err != nil {
		return ""
	}
	if st, err := os.Stat(app); err == nil && !st.IsDir() {
		if isExecutable(st) {
			return filepath.Clean(app)
		}
		return ""
	}
	manifest, err := os.ReadFile(filepath.Join(app, "AppxManifest.xml"))
	if err == nil {
		return manifestAppExecutable(app, manifest)
	}
	if !os.IsNotExist(err) {
		return ""
	}
	for _, name := range []string{"ChatGPT.exe", "Codex.exe"} {
		p := filepath.Join(app, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func manifestAppExecutable(root string, data []byte) string {
	var manifest struct {
		Identity struct {
			Name string `xml:"Name,attr"`
		} `xml:"Identity"`
		Applications []struct {
			ID         string `xml:"Id,attr"`
			Executable string `xml:"Executable,attr"`
		} `xml:"Applications>Application"`
	}
	if xml.Unmarshal(data, &manifest) != nil || manifest.Identity.Name != "OpenAI.Codex" {
		return ""
	}
	for _, app := range manifest.Applications {
		if app.ID != "App" || !filepath.IsLocal(app.Executable) {
			continue
		}
		p := filepath.Join(root, app.Executable)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && isExecutable(st) {
			return p
		}
	}
	return ""
}

func bundledCLIPaths(app string) []string {
	executable := AppExecutable(app)
	if executable == "" {
		return nil
	}
	return []string{filepath.Join(filepath.Dir(executable), "resources", "codex.exe")}
}

func isExecutable(st os.FileInfo) bool {
	ext := filepath.Ext(st.Name())
	return strings.EqualFold(ext, ".exe") || strings.EqualFold(ext, ".com")
}

func ConnectDesktop(context.Context, string, string, []provider.Instance, bool) error {
	return errors.New(DesktopUnsupportedReason)
}
