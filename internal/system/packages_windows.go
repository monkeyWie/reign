package system

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var getPackagePathByFullName = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetPackagePathByFullName")

// Enumerate only packages registered for the current user. The registry supplies
// full names; the package API supplies paths, and AppExecutable validates the
// manifest identity and executable. No shell, service or other user's hive is used.
func registeredPackageRoots(ctx context.Context, name string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\Repository\Packages`, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer key.Close()
	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	roots := []string{}
	for _, fullName := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(fullName, name+"_") {
			continue
		}
		root, e := registeredPackagePath(ctx, fullName)
		// An uninstall can race the enumeration. Other errors are not an
		// empty installation list and must remain visible to the caller.
		if errors.Is(e, windows.ERROR_NOT_FOUND) || errors.Is(e, windows.ERROR_FILE_NOT_FOUND) || errors.Is(e, windows.ERROR_PATH_NOT_FOUND) {
			continue
		}
		if e != nil {
			return nil, e
		}
		roots = append(roots, root)
	}
	return roots, nil
}

func registeredPackagePath(ctx context.Context, fullName string) (string, error) {
	name, err := windows.UTF16PtrFromString(fullName)
	if err != nil {
		return "", err
	}
	var size uint32
	var buf []uint16
	for range 3 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var out *uint16
		if len(buf) > 0 {
			out = &buf[0]
		}
		// This API returns a Win32 status directly, not GetLastError.
		status, _, _ := getPackagePathByFullName.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(out)))
		if status == 0 {
			return windows.UTF16ToString(buf), nil
		}
		if windows.Errno(status) != windows.ERROR_INSUFFICIENT_BUFFER {
			return "", windows.Errno(status)
		}
		if size == 0 || size > 32768 {
			return "", fmt.Errorf("invalid package path length: %d", size)
		}
		buf = make([]uint16, size)
	}
	return "", windows.ERROR_INSUFFICIENT_BUFFER
}
