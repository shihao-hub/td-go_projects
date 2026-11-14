package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	gomessage "github.com/emersion/go-message"
)

// GitHubInfo 是 GitHub 通知邮件中抽取出的结构化信息。
type GitHubInfo struct {
	Repo   string // owner/repo
	Number int    // issue / PR 编号，0 表示未识别
	Kind   string // issue 或 pull
	Title  string
	URL    string
	Actor  string // 触发通知的 GitHub 用户（X-GitHub-Sender）
	Reason string // 通知原因（X-GitHub-Reason）
}

var (
	reRepoName     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	reBracketRepo  = regexp.MustCompile(`\[[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+\]`)
	reIssueURL     = regexp.MustCompile(`https?://(?:www\.)?github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/(issues|pull)/(\d+)`)
	reTrailingNum  = regexp.MustCompile(`\s*\(#(\d+)\)\s*$`)
	reReplyPrefix  = regexp.MustCompile(`(?i)^(?:(?:re|fw|fwd)\s*:\s*)+`)
	reIssueRefHead = regexp.MustCompile(`^#\d+:\s*`)
)

// parseGitHub 判断邮件是否为 GitHub 通知并抽取结构化字段。
// 不是 GitHub 通知时返回 nil。
func parseGitHub(m *Mail, header *gomessage.Header) *GitHubInfo {
	reason := headerText(header, "X-GitHub-Reason")
	actor := headerText(header, "X-GitHub-Sender")
	from := strings.ToLower(m.FromAddress)
	listID := strings.ToLower(m.ListID)

	isGitHub := from == "notifications@github.com" ||
		reason != "" || actor != "" ||
		strings.Contains(listID, "github.com")
	if !isGitHub {
		return nil
	}

	repo := repoFromListID(m.ListID)
	if repo == "" {
		repo = repoFromSubject(m.Subject)
	}

	number := numberFromSubject(m.Subject)
	url, urlRepo, urlNumber, urlKind := findIssueURL(m.Text, repo, number)
	if repo == "" {
		repo = urlRepo
	}
	if number == 0 {
		number = urlNumber
	}
	kind := urlKind
	if kind == "" {
		kind = detectKind(m.Subject)
	}
	if url == "" {
		url = buildIssueURL(repo, number, kind)
	}

	return &GitHubInfo{
		Repo:   repo,
		Number: number,
		Kind:   kind,
		Title:  titleFromSubject(m.Subject),
		URL:    url,
		Actor:  actor,
		Reason: reason,
	}
}

func repoFromListID(listID string) string {
	value := strings.TrimSpace(listID)
	if idx := strings.IndexByte(value, '<'); idx >= 0 {
		value = value[:idx]
	}
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if reRepoName.MatchString(value) {
		return value
	}
	return ""
}

func repoFromSubject(subject string) string {
	if match := reBracketRepo.FindStringSubmatch(subject); match != nil {
		return strings.Trim(match[0], "[]")
	}
	return ""
}

func numberFromSubject(subject string) int {
	match := reTrailingNum.FindStringSubmatch(subject)
	if match == nil {
		return 0
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return number
}

// findIssueURL 在正文中定位 issue/PR 链接。优先同时匹配已知 repo 与编号的
// 链接，其次只匹配 repo，最后回退到第一个 GitHub issue/PR 链接。
func findIssueURL(text, repo string, number int) (url, foundRepo string, foundNumber int, kind string) {
	matches := reIssueURL.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return "", "", 0, ""
	}
	pick := func(match []string) (string, string, int, string) {
		n, _ := strconv.Atoi(match[4])
		return match[0], match[1] + "/" + match[2], n, kindFromPath(match[3])
	}
	for _, match := range matches {
		candidate := match[1] + "/" + match[2]
		n, _ := strconv.Atoi(match[4])
		if (repo == "" || strings.EqualFold(candidate, repo)) && (number == 0 || n == number) {
			return pick(match)
		}
	}
	if repo != "" {
		for _, match := range matches {
			if strings.EqualFold(match[1]+"/"+match[2], repo) {
				return pick(match)
			}
		}
	}
	return pick(matches[0])
}

func kindFromPath(path string) string {
	if strings.EqualFold(path, "pull") {
		return "pull"
	}
	return "issue"
}

func detectKind(subject string) string {
	lower := strings.ToLower(subject)
	if strings.Contains(lower, "pull request") {
		return "pull"
	}
	return "issue"
}

func buildIssueURL(repo string, number int, kind string) string {
	if repo == "" || number == 0 {
		return ""
	}
	path := "issues"
	if kind == "pull" {
		path = "pull"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%d", repo, path, number)
}

func titleFromSubject(subject string) string {
	title := strings.TrimSpace(subject)
	title = reReplyPrefix.ReplaceAllString(title, "")
	title = reBracketRepo.ReplaceAllString(title, "")
	title = reIssueRefHead.ReplaceAllString(title, "")
	title = reTrailingNum.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	for _, prefix := range []string{
		"New issue: ",
		"New pull request: ",
		"New comment on issue ",
		"New comment on pull request ",
	} {
		if len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
			title = title[len(prefix):]
			break
		}
	}
	return strings.TrimSpace(title)
}
