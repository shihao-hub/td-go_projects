package session

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// DefaultRootBranchID 是默认根分支标识符。
const DefaultRootBranchID = "main"

// Branch 表示持久化对话分支。
type Branch struct {
	ID               string    `json:"id"`
	ParentID         string    `json:"parent_id,omitempty"`
	ForkMessageIndex int       `json:"fork_message_index"`
	Messages         []Message `json:"messages"`
	CreatedAt        time.Time `json:"created_at"`
}

// GetBranch 从分支切片中按 ID 查找分支。
func GetBranch(branchID string, branches []Branch) (*Branch, bool) {
	for i := range branches {
		if branches[i].ID == branchID {
			return &branches[i], true
		}
	}
	return nil, false
}

// ResolveMessages 解析指定分支从根节点到当前分支的完整消息历史。
func ResolveMessages(branchID string, branches []Branch) ([]Message, error) {
	visited := make(map[string]bool)
	return resolveMessagesInternal(branchID, branches, visited)
}

func resolveMessagesInternal(branchID string, branches []Branch, visited map[string]bool) ([]Message, error) {
	if branchID == "" {
		return nil, fmt.Errorf("分支 ID 不能为空")
	}
	if visited[branchID] {
		return nil, fmt.Errorf("检测到分支循环依赖: %s", branchID)
	}
	visited[branchID] = true

	branch, ok := GetBranch(branchID, branches)
	if !ok {
		return nil, fmt.Errorf("未找到分支: %s", branchID)
	}

	if branch.ParentID == "" {
		out := make([]Message, len(branch.Messages))
		copy(out, branch.Messages)
		return out, nil
	}

	parentHistory, err := resolveMessagesInternal(branch.ParentID, branches, visited)
	if err != nil {
		return nil, err
	}

	if branch.ForkMessageIndex < 0 || branch.ForkMessageIndex > len(parentHistory) {
		return nil, fmt.Errorf("分支 %s 的 Fork 索引 %d 超出父分支消息总数 %d", branch.ID, branch.ForkMessageIndex, len(parentHistory))
	}

	prefix := parentHistory[:branch.ForkMessageIndex]
	out := make([]Message, len(prefix)+len(branch.Messages))
	copy(out, prefix)
	copy(out[len(prefix):], branch.Messages)
	return out, nil
}

// ValidateBranches 验证分支树的合法性与拓扑结构。
func ValidateBranches(rootID string, branches []Branch) error {
	if rootID == "" {
		return fmt.Errorf("根分支 ID 不能为空")
	}
	if len(branches) == 0 {
		return fmt.Errorf("分支列表不能为空")
	}

	idMap := make(map[string]bool, len(branches))
	rootCount := 0

	for _, b := range branches {
		if b.ID == "" {
			return fmt.Errorf("分支 ID 不能为空")
		}
		if idMap[b.ID] {
			return fmt.Errorf("分支 ID 重复: %s", b.ID)
		}
		idMap[b.ID] = true

		if b.ParentID == "" {
			rootCount++
			if b.ID != rootID {
				return fmt.Errorf("根分支 ID %s 与声明的根分支 ID %s 不一致", b.ID, rootID)
			}
			if b.ForkMessageIndex != 0 {
				return fmt.Errorf("根分支的 Fork 索引必须为 0")
			}
		}
	}

	if rootCount != 1 {
		return fmt.Errorf("必须且只能有一个根分支，当前找到 %d 个", rootCount)
	}

	// 验证所有非根分支的父分支存在且无环，且 Fork 索引合法
	for _, b := range branches {
		if _, err := ResolveMessages(b.ID, branches); err != nil {
			return err
		}
	}

	return nil
}

// GenerateBranchID 根据父分支生成稳定短分支 ID（例如 main -> fork-01, fork-01 -> fork-01-01）。
func GenerateBranchID(parentID string, existing []Branch) string {
	prefix := "fork-"
	if parentID != "" && parentID != DefaultRootBranchID {
		prefix = parentID + "-"
	}

	escapedPrefix := regexp.QuoteMeta(prefix)
	pattern := regexp.MustCompile(`^` + escapedPrefix + `(\d+)$`)

	maxNum := 0
	for _, b := range existing {
		matches := pattern.FindStringSubmatch(b.ID)
		if len(matches) == 2 {
			if n, err := strconv.Atoi(matches[1]); err == nil && n > maxNum {
				maxNum = n
			}
		}
	}

	return fmt.Sprintf("%s%02d", prefix, maxNum+1)
}

// CreateBranch 创建合法子分支并校验父历史。
func CreateBranch(parentID string, forkMessageIndex int, existing []Branch, now time.Time) (Branch, error) {
	if parentID == "" {
		return Branch{}, fmt.Errorf("父分支 ID 不能为空")
	}

	parentHistory, err := ResolveMessages(parentID, existing)
	if err != nil {
		return Branch{}, fmt.Errorf("无法解析父分支 %s 的历史: %w", parentID, err)
	}

	if forkMessageIndex < 0 || forkMessageIndex > len(parentHistory) {
		return Branch{}, fmt.Errorf("Fork 索引 %d 超出父分支消息总数 %d", forkMessageIndex, len(parentHistory))
	}

	newID := GenerateBranchID(parentID, existing)
	return Branch{
		ID:               newID,
		ParentID:         parentID,
		ForkMessageIndex: forkMessageIndex,
		Messages:         make([]Message, 0),
		CreatedAt:        now,
	}, nil
}
