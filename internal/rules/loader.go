package rules

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LoadOptions 是 Load 的输入参数。
//
// 文件不存在不算错误，loader 静默跳过；解析失败不阻断，conflicts 由 parser 写入 Parsed.Conflicts。
type LoadOptions struct {
	// RulesFS 是 assets/rules 子树。约定根目录直接包含 default.md。
	// 通常通过 fs.Sub(embedFS, "rules") 得到；nil 表示跳过内置规则。
	RulesFS fs.FS

	// HomeRulesPath 是 ~/.ainovel/rules.md 的绝对路径；空表示跳过。
	HomeRulesPath string

	// ProjectRulesPath 是 ./rules.md（或调用方指定的项目根）；空表示跳过。
	ProjectRulesPath string
}

// Load 按 Default → Global → Project 顺序读取，返回升序排好的 Parsed 列表。
//
// merger 接收返回值后只需按列表顺序合并即可，后者覆盖前者。
// 不引入二阶段加载——Genre / Learned 等扩展层在真有内容前不开洞。
func Load(opts LoadOptions) []Parsed {
	var layers []Parsed
	if p, ok := readFromFS(opts.RulesFS, "default.md", SourceDefault, "assets/rules/default.md"); ok {
		layers = append(layers, p)
	}
	if p, ok := readFromDisk(opts.HomeRulesPath, SourceGlobal); ok {
		layers = append(layers, p)
	}
	if p, ok := readFromDisk(opts.ProjectRulesPath, SourceProject); ok {
		layers = append(layers, p)
	}
	return layers
}

// readFromFS 从 fs.FS 读取并解析；文件不存在返回 (Parsed{}, false)。
// displayPath 用于 Parsed.Source（便于在 sources/conflicts 里显示为 "assets/rules/..."）。
func readFromFS(fsys fs.FS, name string, kind SourceKind, displayPath string) (Parsed, bool) {
	if fsys == nil {
		return Parsed{}, false
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		// 文件不存在静默跳过；其他错误也不阻断（loader 设计上不报错）
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return Parsed{}, false
		}
		// 极少数 IO 错误：作为 parse_error 暴露，避免静默
		return Parsed{
			Source: displayPath,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: displayPath,
				Kind:   ConflictParseError,
				Detail: "读取失败: " + err.Error(),
			}},
		}, true
	}
	return Parse(displayPath, kind, data), true
}

// readFromDisk 从绝对路径读取并解析；空路径或文件不存在返回 (Parsed{}, false)。
func readFromDisk(absPath string, kind SourceKind) (Parsed, bool) {
	if strings.TrimSpace(absPath) == "" {
		return Parsed{}, false
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Parsed{}, false
		}
		return Parsed{
			Source: absPath,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: absPath,
				Kind:   ConflictParseError,
				Detail: "读取失败: " + err.Error(),
			}},
		}, true
	}
	return Parse(absPath, kind, data), true
}

// DefaultProjectRulesPath 拼出 ./rules.md 的绝对路径（基于给定项目目录）。
// 调用方传入项目根，避免在 loader 内部依赖 cwd。
func DefaultProjectRulesPath(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	return filepath.Join(projectDir, "rules.md")
}

// DefaultHomeRulesPath 拼出 ~/.ainovel/rules.md 的绝对路径。
// home 解析失败返回空串（调用方据此跳过该来源）。
func DefaultHomeRulesPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ainovel", "rules.md")
}

// DefaultOptions 根据当前工作目录构造常用 LoadOptions。
//
// 适合 Host 启动时调用一次，让 ContextTool / CommitChapterTool 复用同一份配置。
// 解析 cwd 失败时 ProjectRulesPath 留空（loader 会跳过该来源）。
//
// 路径语义：ProjectRulesPath 绑定 **当前工作目录（cwd）** 而非 outputDir。
// 用户 cd 到不同目录启动写不同的书，./rules.md 自然跟着 cwd 走；如需跨书共享，
// 放 ~/.ainovel/rules.md 全局层即可。
func DefaultOptions(rulesFS fs.FS) LoadOptions {
	cwd, _ := os.Getwd()
	return LoadOptions{
		RulesFS:          rulesFS,
		HomeRulesPath:    DefaultHomeRulesPath(),
		ProjectRulesPath: DefaultProjectRulesPath(cwd),
	}
}
