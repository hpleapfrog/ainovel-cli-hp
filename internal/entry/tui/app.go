package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/logger"
)

// Run 启动 TUI。
// 启动模式分层约定：
// 1. 快速模式、共创模式属于“启动编排”；
// 2. 正式创作会话进入 host.Host；
// 3. 未来若新增“续写已有小说”等共享模式，统一落到 internal/entry/startup。
func Run(cfg bootstrap.Config, bundle assets.Bundle, version string) error {
	// Windows cmd.exe / PowerShell（非 Windows Terminal）默认不启用 VT 处理，
	// 导致 ANSI 转义序列被当普通文本输出，画面叠加/撕裂。必须在创建
	// bubbletea 程序前启用（内部失败按设计静默，不阻断启动）。
	enableVirtualTerminalProcessing()
	defer restoreMainScreen()

	rt, err := host.New(cfg, bundle)
	if err != nil {
		return err
	}
	bridge := newAskUserBridge()
	rt.AskUser().SetHandler(bridge.handler)
	cleanup := logger.SetupFile(rt.Dir(), "tui.log", false)
	defer cleanup()
	defer rt.Close()

	m := NewModel(rt, bridge, version)
	// 不在启动时全局开启鼠标上报：欢迎页用不到鼠标，关闭上报可保留终端原生
	// 拖拽选中复制。进入创作工作台（modeRunning）时再由 enterRunning 打开上报，
	// 以支持点击切面板 / 滚轮 / 拖拽侧边栏。
	//
	// 旧版 Windows 控制台主机的 \033[?1049h（备用屏幕缓冲区）序列有 bug，
	// 即使启用 VT 处理也会导致画面叠加错位。此时跳过 AltScreen 模式，
	// bubbletea 回退到标准模式渲染（\033[NA 光标上移 + \033[J 擦除下方），
	// 这些基础 VT 序列在旧版控制台上可正常工作。
	opts := []tea.ProgramOption{}
	if !needsWin32AltScreen() {
		opts = append(opts, tea.WithAltScreen())
	}
	p := tea.NewProgram(m, opts...)
	_, err = p.Run()
	return err
}
