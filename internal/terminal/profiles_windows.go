package terminal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const SupportsProfiles = true

func Profiles(ctx context.Context) ([]Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	profiles := []Profile{}
	seen := map[string]bool{}
	add := func(id, name, path string) {
		if seen[id] || !filepath.IsAbs(path) {
			return
		}
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			profiles = append(profiles, Profile{ID: id, Name: name, path: path})
			seen[id] = true
		}
	}
	add("cmd", "Command Prompt (cmd)", filepath.Join(system, "cmd.exe"))
	add("windows-powershell", "Windows PowerShell", filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"))

	// MSI installations are registered even when that major is not on PATH.
	var dirs []string
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			if git, err := registry.OpenKey(root, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE|view); err == nil {
				dir, _, _ := git.GetStringValue("InstallPath")
				git.Close()
				add("git-bash", "Git Bash", filepath.Join(dir, "bin", "bash.exe"))
			}
			k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\PowerShellCore\InstalledVersions`, registry.READ|view)
			if err != nil {
				continue
			}
			names, _ := k.ReadSubKeyNames(64)
			for _, name := range names {
				entry, err := registry.OpenKey(k, name, registry.QUERY_VALUE|view)
				if err != nil {
					continue
				}
				dir, _, _ := entry.GetStringValue("InstallLocation")
				entry.Close()
				dirs = append(dirs, dir)
			}
			k.Close()
		}
	}
	for _, variable := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		root := os.Getenv(variable)
		if !filepath.IsAbs(root) {
			continue
		}
		add("git-bash", "Git Bash", filepath.Join(root, "Git", "bin", "bash.exe"))
		dirs = append(dirs, filepath.Join(root, "PowerShell", "7"), filepath.Join(root, "PowerShell", "6"), filepath.Join(root, "PowerShell", "7-preview"))
	}
	// Portable installations can coexist in different absolute PATH entries.
	paths := filepath.SplitList(os.Getenv("PATH"))
	dirs = append(dirs, paths...)
	checked := map[string]bool{}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, "pwsh.exe")
		key := strings.ToLower(path)
		if checked[key] {
			continue
		}
		checked[key] = true
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			continue
		}
		// pwsh.dll carries PowerShell's product version even for the .NET apphost.
		major := productMajor(filepath.Join(dir, "pwsh.dll"))
		if major == 0 {
			major = productMajor(path)
		}
		name, id := "PowerShell (pwsh)", "pwsh"
		if major > 0 {
			name, id = fmt.Sprintf("PowerShell %d", major), "pwsh-"+strconv.Itoa(major)
		}
		if strings.Contains(strings.ToLower(filepath.Base(dir)), "preview") {
			name, id = name+" Preview", id+"-preview"
		}
		add(id, name, path)
	}

	// The Windows launcher exists on machines without a configured distro.
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Lxss`, registry.READ); err == nil {
		distro, _, _ := k.GetStringValue("DefaultDistribution")
		if distro != "" {
			if entry, err := registry.OpenKey(k, distro, registry.QUERY_VALUE); err == nil {
				name, _, _ := entry.GetStringValue("DistributionName")
				entry.Close()
				if name != "" {
					add("wsl", "WSL", filepath.Join(system, "wsl.exe"))
				}
			}
		}
		k.Close()
	}
	return profiles, nil
}

func productMajor(path string) int {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 || size > 1<<20 {
		return 0
	}
	buf := make([]byte, size)
	if windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buf[0])) != nil {
		return 0
	}
	var info *windows.VS_FIXEDFILEINFO
	var n uint32
	err = windows.VerQueryValue(unsafe.Pointer(&buf[0]), `\`, unsafe.Pointer(&info), &n)
	if err != nil || info == nil || uintptr(n) < unsafe.Sizeof(*info) || info.Signature != 0xfeef04bd {
		return 0
	}
	return int(info.ProductVersionMS >> 16)
}
