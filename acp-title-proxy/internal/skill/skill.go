package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/protocol"
)

// Skill 表示一个已发现的技能及其元数据与命令定义。
type Skill struct {
	Name           string // 例如 "sh-pocock-workflow"
	CommandName    string // 例如 "sh-pocock-workflow" 或 "skill:sh-pocock-workflow"
	Path           string // SKILL.md 的绝对路径
	Description    string // 清理后的简短描述，供 UI 提示使用
	RawDescription string
}

// Manager 负责技能的发现、缓存与匹配。
type Manager struct {
	cfg        *config.SkillsConfig
	mu         sync.RWMutex
	skills     map[string]*Skill // 键为小写化的 CommandName
	skillsList []*Skill
	lastScan   time.Time
}

func NewManager(cfg *config.SkillsConfig) *Manager {
	if cfg == nil {
		cfg = &config.SkillsConfig{
			Enabled:          true,
			AutoInjectPrompt: true,
		}
	}
	m := &Manager{
		cfg:    cfg,
		skills: make(map[string]*Skill),
	}
	_ = m.Scan()
	return m
}

// Scan 遍历配置目录并发现所有有效技能。
func (m *Manager) Scan() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dirs := m.cfg.Dirs
	if len(dirs) == 0 {
		dirs = config.DefaultSkillDirs()
	}

	newSkills := make(map[string]*Skill)
	var list []*Skill

	for _, rawDir := range dirs {
		expandedDir := config.ExpandEnv(rawDir)
		entries, err := os.ReadDir(expandedDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			dirName := entry.Name()
			// 忽略隐藏目录、测试钩子与 Python 缓存
			if strings.HasPrefix(dirName, ".") || dirName == "__pycache__" || dirName == "node_modules" {
				continue
			}

			skillMdPath := filepath.Join(expandedDir, dirName, "SKILL.md")
			data, err := os.ReadFile(skillMdPath)
			if err != nil {
				continue
			}

			skillName, desc := ParseFrontmatter(string(data))
			if skillName == "" {
				skillName = dirName
			}
			if desc == "" {
				desc = fmt.Sprintf("技能: %s", skillName)
			}

			cmdName := skillName
			if m.cfg.Prefix != "" {
				cmdName = m.cfg.Prefix + skillName
			}

			lowerCmdName := strings.ToLower(cmdName)
			lowerSkillName := strings.ToLower(skillName)
			// 先发现者优先（目录顺序即优先级）
			if _, exists := newSkills[lowerCmdName]; exists {
				continue
			}

			s := &Skill{
				Name:           skillName,
				CommandName:    cmdName,
				Path:           skillMdPath,
				Description:    desc,
				RawDescription: desc,
			}

			newSkills[lowerCmdName] = s
			if _, exists := newSkills[lowerSkillName]; !exists {
				newSkills[lowerSkillName] = s
			}
			list = append(list, s)
		}
	}

	// 按 CommandName 字母序排序，便于 UI 展示
	sort.Slice(list, func(i, j int) bool {
		return list[i].CommandName < list[j].CommandName
	})

	m.skills = newSkills
	m.skillsList = list
	m.lastScan = time.Now()
	return nil
}

// GetCommandItems 返回供 Zed 的 availableCommands 使用的 protocol.CommandItem 列表。
func (m *Manager) GetCommandItems() []protocol.CommandItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.cfg.Enabled {
		return nil
	}

	items := make([]protocol.CommandItem, 0, len(m.skillsList)+1)

	// 追加通用 /skill 命令
	items = append(items, protocol.CommandItem{
		Name:        "skill",
		Description: "指定并激活特定技能 (用法: /skill <name> [任务说明])",
	})

	for _, s := range m.skillsList {
		items = append(items, protocol.CommandItem{
			Name:        s.CommandName,
			Description: s.Description,
		})
	}

	return items
}

// MatchAndRewrite 判断 prompt 是否调用技能，并做技能增强后的重写。
func (m *Manager) MatchAndRewrite(rawPrompt string) (bool, string, string, *Skill) {
	if !m.cfg.Enabled || !m.cfg.AutoInjectPrompt {
		return false, rawPrompt, rawPrompt, nil
	}

	trimmed := strings.TrimSpace(rawPrompt)
	if !strings.HasPrefix(trimmed, "/") {
		return false, rawPrompt, rawPrompt, nil
	}

	lineWithoutSlash := strings.TrimPrefix(trimmed, "/")
	fields := strings.Fields(lineWithoutSlash)
	if len(fields) == 0 {
		return false, rawPrompt, rawPrompt, nil
	}

	firstToken := fields[0]
	lowerFirstToken := strings.ToLower(firstToken)

	var targetSkill *Skill
	var userTask string

	m.mu.RLock()
	defer m.mu.RUnlock()

	// 模式 1：通用语法 "/skill <name> [用户任务]"
	if lowerFirstToken == "skill" && len(fields) >= 2 {
		skillArg := strings.ToLower(fields[1])
		if s, ok := m.skills[skillArg]; ok {
			targetSkill = s
		} else if s, ok := m.skills["skill:"+skillArg]; ok {
			targetSkill = s
		}

		if targetSkill != nil {
			prefixLen := len("/skill") + len(fields[1]) + 1
			for prefixLen < len(trimmed) && trimmed[prefixLen] == ' ' {
				prefixLen++
			}
			if prefixLen < len(trimmed) {
				userTask = strings.TrimSpace(trimmed[prefixLen:])
			}
		}
	}

	// 模式 2：直接技能命令 "/sh-pocock-workflow [用户任务]"
	if targetSkill == nil {
		if s, ok := m.skills[lowerFirstToken]; ok {
			targetSkill = s
			prefixLen := len(firstToken) + 1
			for prefixLen < len(trimmed) && trimmed[prefixLen] == ' ' {
				prefixLen++
			}
			if prefixLen < len(trimmed) {
				userTask = strings.TrimSpace(trimmed[prefixLen:])
			}
		}
	}

	if targetSkill == nil {
		return false, rawPrompt, rawPrompt, nil
	}

	// 构造增强后的 prompt
	rewritten := m.buildPrompt(targetSkill, userTask)
	return true, rewritten, userTask, targetSkill
}

func (m *Manager) buildPrompt(s *Skill, userTask string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("【系统注入：激活技能 %s】\n", s.Name))
	sb.WriteString(fmt.Sprintf("- 技能定义文档路径：%s\n", s.Path))
	sb.WriteString(fmt.Sprintf("- 技能概述：%s\n", s.Description))

	if userTask != "" {
		sb.WriteString("- 执行指引：在处理任务前，请优先使用 read_file 工具阅读上述技能定义文档，并严格遵循其工作流与规范执行。\n\n")
		sb.WriteString("【用户具体任务】\n")
		sb.WriteString(userTask)
	} else {
		sb.WriteString("- 执行指引：请优先使用 read_file 工具阅读上述技能定义文档，向我确认你已就绪，并简要概述你能提供的工作流与辅助能力。")
	}

	return sb.String()
}
