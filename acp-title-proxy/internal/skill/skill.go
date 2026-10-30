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

// Skill represents a discovered skill with its metadata and command definition.
type Skill struct {
	Name           string // e.g. "sh-pocock-workflow"
	CommandName    string // e.g. "sh-pocock-workflow" or "skill:sh-pocock-workflow"
	Path           string // Absolute path to SKILL.md
	Description    string // Cleaned short description for UI tooltip
	RawDescription string
}

// Manager manages discovering, caching and matching skills.
type Manager struct {
	cfg        *config.SkillsConfig
	mu         sync.RWMutex
	skills     map[string]*Skill // key: lower-cased CommandName
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

// Scan traverses configured directories and discovers all valid skills.
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
			// Ignore hidden directories, test hooks and python caches
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
			// First discovered (earlier dir in precedence) wins
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

	// Sort skills alphabetically by CommandName for clean UI display
	sort.Slice(list, func(i, j int) bool {
		return list[i].CommandName < list[j].CommandName
	})

	m.skills = newSkills
	m.skillsList = list
	m.lastScan = time.Now()
	return nil
}

// GetCommandItems returns protocol.CommandItem slice for Zed's availableCommands.
func (m *Manager) GetCommandItems() []protocol.CommandItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.cfg.Enabled {
		return nil
	}

	items := make([]protocol.CommandItem, 0, len(m.skillsList)+1)

	// Add generic /skill command
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

// MatchAndRewrite checks if the prompt invokes a skill and rewrites it with prompt augmentation.
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

	// Mode 1: Generic syntax "/skill <name> [user task]"
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

	// Mode 2: Direct skill command "/sh-pocock-workflow [user task]"
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

	// Build augmented prompt
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
