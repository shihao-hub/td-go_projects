// Package parser 解析 IMAP 拉取到的原始邮件：通用头/正文抽取，
// 以及 GitHub 通知邮件的专有字段识别。
package parser

import (
	"bytes"
	"fmt"
	"html"
	"io"
	netmail "net/mail"
	"regexp"
	"strings"
	"time"

	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // 注册 GBK 等字符集解码器
	"github.com/emersion/go-message/mail"
)

// maxBodyBytes 限制单个 MIME 文本部件的读取上限，防止超大邮件撑爆内存。
const maxBodyBytes = 1 << 20

// Mail 是一封邮件的通用解析结果。
type Mail struct {
	From        string // 原始 From 头（含显示名）
	FromAddress string // 小写邮箱地址
	Subject     string
	Date        time.Time
	MessageID   string
	ListID      string
	Text        string // 清理后的纯文本正文
	GitHub      *GitHubInfo
}

// Parse 解析原始邮件字节。MIME 结构异常时返回错误；正文解析失败时
// 尽量保留已读取的头信息，由调用方决定是否跳过。
func Parse(raw []byte) (*Mail, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("邮件内容为空")
	}
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解析邮件 MIME 结构失败: %w", err)
	}
	defer reader.Close()

	header := &reader.Header

	subject, _ := header.Subject()
	fromRaw := strings.TrimSpace(header.Get("From"))
	date, _ := header.Date()
	messageID, _ := header.MessageID()
	listID := headerText(&header.Header, "List-Id")

	plain, htmlBody := readBodies(reader)
	text := strings.TrimSpace(plain)
	if text == "" && htmlBody != "" {
		text = stripHTML(htmlBody)
	}
	text = cleanText(text)

	parsed := &Mail{
		From:        fromRaw,
		FromAddress: addressOf(fromRaw),
		Subject:     strings.TrimSpace(subject),
		Date:        date,
		MessageID:   strings.TrimSpace(messageID),
		ListID:      listID,
		Text:        text,
	}
	parsed.GitHub = parseGitHub(parsed, &header.Header)
	return parsed, nil
}

// readBodies 依次扫描 MIME 部件，优先 text/plain，其次 text/html。
func readBodies(reader *mail.Reader) (plain, htmlBody string) {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		contentType, _, err := inline.ContentType()
		if err != nil {
			contentType = "text/plain"
		}
		switch strings.ToLower(contentType) {
		case "text/plain":
			if strings.TrimSpace(plain) != "" {
				continue
			}
			if data, err := io.ReadAll(io.LimitReader(part.Body, maxBodyBytes)); err == nil {
				plain = string(data)
			}
		case "text/html":
			if htmlBody != "" || strings.TrimSpace(plain) != "" {
				continue
			}
			if data, err := io.ReadAll(io.LimitReader(part.Body, maxBodyBytes)); err == nil {
				htmlBody = string(data)
			}
		}
	}
	return plain, htmlBody
}

func headerText(header *gomessage.Header, key string) string {
	value, err := header.Text(key)
	if err != nil && value == "" {
		return strings.TrimSpace(header.Get(key))
	}
	return strings.TrimSpace(value)
}

func addressOf(raw string) string {
	if raw == "" {
		return ""
	}
	addr, err := netmail.ParseAddress(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr.Address))
}

var (
	reScriptStyle = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
	reHTMLBreak   = regexp.MustCompile(`(?i)<br\s*/?>|</p\s*>|</div\s*>|</li\s*>|</tr\s*>|</h[1-6]\s*>`)
	reHTMLTag     = regexp.MustCompile(`(?s)<[^>]+>`)
)

// stripHTML 做最小化的 HTML 转文本，只服务于"没有 text/plain 时也能读出摘要"。
func stripHTML(value string) string {
	value = reScriptStyle.ReplaceAllString(value, " ")
	value = reHTMLBreak.ReplaceAllString(value, "\n")
	value = reHTMLTag.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

// cleanText 统一换行、去除 GitHub 通知尾部样板文字并折叠多余空行。
func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")

	lines := strings.Split(value, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "You are receiving this because you") {
			break
		}
		if strings.HasPrefix(trimmed, "Reply to this email directly") {
			break
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}

	for len(kept) > 0 {
		last := strings.TrimSpace(kept[len(kept)-1])
		if last == "" || last == "--" {
			kept = kept[:len(kept)-1]
			continue
		}
		break
	}

	collapsed := make([]string, 0, len(kept))
	blank := 0
	for _, line := range kept {
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		collapsed = append(collapsed, line)
	}
	return strings.TrimSpace(strings.Join(collapsed, "\n"))
}

// Summary 返回正文的前 limit 个字符，供卡片正文使用。
func Summary(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}
