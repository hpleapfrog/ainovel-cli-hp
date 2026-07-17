//go:build !windows

package tui

// enableVirtualTerminalProcessing 在非 Windows 平台上是空操作。
// Unix/Linux/macOS 终端默认支持 VT 转义序列。
func enableVirtualTerminalProcessing() {}

// restoreMainScreen 在非 Windows 平台上是空操作。
func restoreMainScreen() {}

// needsWin32AltScreen 在非 Windows 平台永远返回 false。
func needsWin32AltScreen() bool {
	return false
}
