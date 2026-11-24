//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkcu          = uintptr(0x80000001)
	keyQueryValue = 0x0001
	keySetValue   = 0x0002
	regSZ         = 1
)

var advapi = syscall.NewLazyDLL("advapi32.dll")

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValue = "HoverDict"

func openRunKey() (uintptr, error) {
	var h uintptr
	r, _, _ := advapi.NewProc("RegOpenKeyExW").Call(hkcu, ptr(runKey), 0, keyQueryValue|keySetValue, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return h, nil
}
func getAutoStart() (bool, error) {
	k, err := openRunKey()
	if err != nil {
		return false, err
	}
	defer advapi.NewProc("RegCloseKey").Call(k)
	var kind uint32
	var lenBytes uint32
	r, _, _ := advapi.NewProc("RegQueryValueExW").Call(k, ptr(runValue), 0, uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&lenBytes)))
	if r == 2 {
		return false, nil
	} // ERROR_FILE_NOT_FOUND
	if r != 0 {
		return false, syscall.Errno(r)
	}
	if kind != regSZ || lenBytes < 2 || lenBytes > 4096 {
		return false, fmt.Errorf("开机启动项内容无效")
	}
	value := make([]uint16, (lenBytes+1)/2)
	r, _, _ = advapi.NewProc("RegQueryValueExW").Call(k, ptr(runValue), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&value[0])), uintptr(unsafe.Pointer(&lenBytes)))
	if r != 0 {
		return false, syscall.Errno(r)
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(syscall.UTF16ToString(value)), `"`+exe+`"`), nil
}
func setAutoStart(enabled bool) error {
	k, err := openRunKey()
	if err != nil {
		return err
	}
	defer advapi.NewProc("RegCloseKey").Call(k)
	if !enabled {
		r, _, _ := advapi.NewProc("RegDeleteValueW").Call(k, ptr(runValue))
		if r != 0 && r != 2 {
			return syscall.Errno(r)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	data := syscall.StringToUTF16(`"` + exe + `"`)
	r, _, _ := advapi.NewProc("RegSetValueExW").Call(k, ptr(runValue), 0, regSZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// Enabled on first run; if the Run entry points at a moved EXE, refresh it.
// Respect user removal after initial setup.
func initializeStartupDefault() {
	if layoutState.StartupInitialized {
		exists, err := autoStartValueExists()
		if err == nil && exists {
			samePath, err := getAutoStart()
			if err == nil && !samePath {
				_ = setAutoStart(true)
			}
		}
		return
	}
	if err := setAutoStart(true); err != nil {
		logDiagnostic("autostart", err.Error())
		return
	}
	layoutState.StartupInitialized = true
	saveLayout()
}

// Detect an existing Run value without treating a user-disabled entry as missing.
func autoStartValueExists() (bool, error) {
	key, err := openRunKey()
	if err != nil {
		return false, err
	}
	defer advapi.NewProc("RegCloseKey").Call(key)
	var size uint32
	r, _, _ := advapi.NewProc("RegQueryValueExW").Call(key, ptr(runValue), 0, 0, 0, uintptr(unsafe.Pointer(&size)))
	if r == 2 {
		return false, nil
	}
	if r != 0 {
		return false, syscall.Errno(r)
	}
	return true, nil
}
