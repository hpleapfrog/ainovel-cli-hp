//go:build windows

package tui

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var vtOnce sync.Once

var (
	kernel32                         = syscall.NewLazyDLL("kernel32.dll")
	procCreateConsoleScreenBuffer    = kernel32.NewProc("CreateConsoleScreenBuffer")
	procSetConsoleActiveScreenBuffer = kernel32.NewProc("SetConsoleActiveScreenBuffer")
	procFillConsoleOutputCharacterW  = kernel32.NewProc("FillConsoleOutputCharacterW")
	procFillConsoleOutputAttribute   = kernel32.NewProc("FillConsoleOutputAttribute")
	procSetConsoleCursorPosition     = kernel32.NewProc("SetConsoleCursorPosition")
	procGetConsoleScreenBufferInfo   = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

type consoleCoord struct{ X, Y int16 }

type consoleScreenBufferInfo struct {
	Size              consoleCoord
	CursorPosition    consoleCoord
	Attributes        uint16
	Window            struct{ Left, Top, Right, Bottom int16 }
	MaximumWindowSize consoleCoord
}

var altBufHandle windows.Handle
var origStdout *os.File
var usesFallback bool

// enableVirtualTerminalProcessing 尽力启用 VT 处理；所有失败按设计静默
// （重定向 stdout、无 console 等场景不应阻断 TUI 启动），故无错误返回。
func enableVirtualTerminalProcessing() {
	vtOnce.Do(initWindowsConsole)
}

func initWindowsConsole() {
	windows.SetConsoleOutputCP(65001)
	windows.SetConsoleCP(65001)
	forceModernConsole()
	enableVTOnAllHandles()

	if isLegacyConsole() {
		// 只有备用缓冲区真正创建成功才降级：MSYS/MinTTY 无 console 可创建，
		// 失败时保持 bubbletea 原生 AltScreen，不误判为 legacy 渲染
		usesFallback = createAltScreenBuffer()
	}
}

// forceModernConsole 为当前用户永久启用新控制台的 VT 支持。
// 注意：这会永久写入注册表 HKCU\Console\VirtualTerminalLevel=1，影响该用户
// 此后所有新控制台会话（不只是本程序）。属一次性环境修复，重复运行幂等。
func forceModernConsole() {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Console`, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if val, _, err := k.GetIntegerValue("VirtualTerminalLevel"); err == nil && val == 1 {
		return
	}
	_ = k.SetDWordValue("VirtualTerminalLevel", 1)
}

func isLegacyConsole() bool {
	if os.Getenv("WT_SESSION") != "" {
		return false
	}
	if os.Getenv("ConEmuANSI") == "ON" {
		return false
	}
	if os.Getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm" {
		return false
	}
	for _, env := range []string{"WEZTERM_PANE", "ALACRITTY_LOG", "TERM_PROGRAM"} {
		if os.Getenv(env) != "" {
			return false
		}
	}
	return true
}

func enableVTOnAllHandles() {
	for _, fd := range []uintptr{os.Stdout.Fd(), os.Stderr.Fd()} {
		enableVTOnHandle(windows.Handle(fd))
	}
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil {
		enableVTOnHandle(h)
	}
	if h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil {
		enableVTOnHandle(h)
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		enableVTOnHandle(windows.Handle(f.Fd()))
		f.Close()
	}
}

func enableVTOnHandle(h windows.Handle) {
	if h == windows.InvalidHandle {
		return
	}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	const vtProcessing = 0x0004
	const noAutoReturn = 0x0008
	want := mode | vtProcessing | noAutoReturn
	if want == mode {
		return
	}
	_ = windows.SetConsoleMode(h, want)
}

func createAltScreenBuffer() bool {
	const (
		genericRead  = 0x80000000
		genericWrite = 0x40000000
		shareMode    = 0x00000001 | 0x00000002
	)

	h, _, _ := procCreateConsoleScreenBuffer.Call(
		genericRead|genericWrite, shareMode, 0, 1, 0,
	)
	if h == 0 || h == ^uintptr(0) {
		return false
	}

	var info consoleScreenBufferInfo
	if hStd, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil {
		procGetConsoleScreenBufferInfo.Call(uintptr(hStd), uintptr(unsafe.Pointer(&info)))
	}

	enableVTOnHandle(windows.Handle(h))

	x, y := int32(info.Size.X), int32(info.Size.Y)
	if x <= 0 || y <= 0 {
		x, y = 80, 25
	}
	total := uint32(x * y)
	var written uint32
	origin := consoleCoord{0, 0}
	// Win32 COORD 是按值传递的 32 位打包值（低 16 位 X、高 16 位 Y），不是指针
	coordValue := uintptr(uint32(uint16(origin.Y))<<16 | uint32(uint16(origin.X)))
	procFillConsoleOutputCharacterW.Call(h, uintptr(' '), uintptr(total), coordValue, uintptr(unsafe.Pointer(&written)))
	procFillConsoleOutputAttribute.Call(h, 7, uintptr(total), coordValue, uintptr(unsafe.Pointer(&written)))
	procSetConsoleCursorPosition.Call(h, coordValue)

	origStdout = os.Stdout
	os.Stdout = os.NewFile(uintptr(h), "CONOUT$")
	procSetConsoleActiveScreenBuffer.Call(h)
	altBufHandle = windows.Handle(h)
	return true
}

func needsWin32AltScreen() bool {
	return usesFallback
}

func restoreMainScreen() {
	if altBufHandle == 0 {
		return
	}
	if origStdout != nil {
		procSetConsoleActiveScreenBuffer.Call(origStdout.Fd())
		os.Stdout = origStdout
	}
	windows.CloseHandle(altBufHandle)
	altBufHandle = 0
	usesFallback = false
}
