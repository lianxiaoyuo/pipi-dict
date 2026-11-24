//go:build windows

package main

import (
	"errors"
	"fmt"
	"hoverdict/internal/filelock"
	"os"
	"path/filepath"
)

const wmShowRunning = 0x8003

func acquireAppInstance() (*filelock.Lock, bool, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, false, err
	}
	lockPath := filepath.Join(root, "HoverDict", "instance.lock")
	lock, err := filelock.TryAcquire(lockPath)
	if err == nil {
		// V3.2 and earlier had no instance.lock. If an old window exists,
		// do not start a V3.3 writer beside it; V3.2 does not honor the new
		// transaction locks and could otherwise still overwrite bookmarks.
		previous := call(u32.NewProc("FindWindowW"), ptr("HoverDictNative1"), 0)
		if previous != 0 {
			lock.Close()
			call(u32.NewProc("MessageBoxW"), 0, ptr("发现旧版 HoverDict 仍在运行。请先在旧版中按 Ctrl+Alt+Q 完全退出，再启动 V4.6，以免同时修改生词本。"), ptr("HoverDict 升级保护"), 0x30)
			return nil, true, nil
		}
		return lock, false, nil
	}
	if !errors.Is(err, filelock.ErrLocked) {
		return nil, false, err
	}
	// The other window may still be importing the dictionary; treat that as
	// already running instead of racing it or starting a second writer.
	existing := call(u32.NewProc("FindWindowW"), ptr("HoverDictNative1"), 0)
	if existing != 0 {
		call(u32.NewProc("PostMessageW"), existing, wmShowRunning, 0, 0)
	} else {
		call(u32.NewProc("MessageBoxW"), 0, ptr("HoverDict 已经在运行或正在加载词库，请使用已有窗口。"), ptr("HoverDict"), 0x40)
	}
	return nil, true, nil
}
func showStartupError(err error) {
	text := fmt.Sprintf("HoverDict 无法安全启动：%v\n请检查 %%APPDATA%%\\HoverDict 目录权限。", err)
	call(u32.NewProc("MessageBoxW"), 0, ptr(text), ptr("HoverDict 启动错误"), 0x10)
}
