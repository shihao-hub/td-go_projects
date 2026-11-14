// Package matcher 实现零信任白名单匹配。
//
// 匹配语义：
//   - 规则之间 OR：命中任意一条规则即放行；
//   - 规则内部：类别之间 AND（邮箱、域名、关键词三类条件都必须满足，
//     未配置的类别视为不约束），同一类别内的多个值之间 OR；
//   - 未命中任何规则、或 From 无法解析时一律阻断（默认全阻断）。
//
// 空规则（三类条件全部为空）在构造时报错，避免误配置成"匹配一切"。
package matcher

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Rule 是一条白名单规则。同一规则内的多类条件之间是 AND 关系。
type Rule struct {
	Name            string   `yaml:"name" json:"name"`
	SenderEmails    []string `yaml:"sender_emails" json:"sender_emails"`
	SenderDomains   []string `yaml:"sender_domains" json:"sender_domains"`
	SubjectKeywords []string `yaml:"subject_keywords" json:"subject_keywords"`
}

// Validate 检查规则本身是否可用；必须在构造匹配器前通过。
func (r Rule) Validate() error {
	if len(r.SenderEmails)+len(r.SenderDomains)+len(r.SubjectKeywords) == 0 {
		return errors.New("规则至少需要一个匹配条件（sender_emails / sender_domains / subject_keywords）")
	}
	for _, raw := range r.SenderEmails {
		addr, err := parseAddress(raw)
		if err != nil {
			return fmt.Errorf("非法发件人邮箱 %q: %w", raw, err)
		}
		if addr.domain == "" {
			return fmt.Errorf("发件人邮箱缺少域名: %q", raw)
		}
	}
	for _, domain := range r.SenderDomains {
		d := strings.TrimSpace(strings.ToLower(domain))
		if d == "" {
			return errors.New("发件人域名不能为空")
		}
		if strings.HasPrefix(d, "@") || strings.Contains(d, " ") || strings.Contains(d, "/") {
			return fmt.Errorf("非法发件人域名 %q（只写域名本身，如 github.com）", domain)
		}
	}
	for _, keyword := range r.SubjectKeywords {
		if strings.TrimSpace(keyword) == "" {
			return errors.New("主题关键词不能为空")
		}
	}
	return nil
}

// Result 描述命中的规则。
type Result struct {
	Rule string
}

type compiledRule struct {
	name     string
	emails   map[string]struct{}
	domains  []string
	keywords []string
}

// Matcher 是编译后的白名单匹配器。
type Matcher struct {
	rules []compiledRule
}

// New 编译白名单规则。规则非法时返回错误而不是静默跳过。
func New(rules []Rule) (*Matcher, error) {
	m := &Matcher{rules: make([]compiledRule, 0, len(rules))}
	for i, rule := range rules {
		if err := rule.Validate(); err != nil {
			return nil, fmt.Errorf("第 %d 条规则无效: %w", i+1, err)
		}
		compiled := compiledRule{
			name:     rule.Name,
			emails:   make(map[string]struct{}, len(rule.SenderEmails)),
			domains:  make([]string, 0, len(rule.SenderDomains)),
			keywords: make([]string, 0, len(rule.SubjectKeywords)),
		}
		for _, raw := range rule.SenderEmails {
			addr, err := parseAddress(raw)
			if err != nil {
				return nil, fmt.Errorf("第 %d 条规则无效: %w", i+1, err)
			}
			compiled.emails[addr.address] = struct{}{}
		}
		for _, domain := range rule.SenderDomains {
			compiled.domains = append(compiled.domains, strings.ToLower(strings.TrimSpace(domain)))
		}
		for _, keyword := range rule.SubjectKeywords {
			compiled.keywords = append(compiled.keywords, strings.ToLower(strings.TrimSpace(keyword)))
		}
		m.rules = append(m.rules, compiled)
	}
	return m, nil
}

// Match 判断一封邮件是否通过白名单。from 为原始 From 头，subject 为主题。
func (m *Matcher) Match(from, subject string) (Result, bool) {
	addr, err := parseAddress(from)
	if err != nil {
		// 无法确认来源时按阻断处理，符合零信任语义。
		return Result{}, false
	}
	email := strings.ToLower(addr.address)
	lowerSubject := strings.ToLower(subject)
	for _, rule := range m.rules {
		if rule.matches(email, addr.domain, lowerSubject) {
			return Result{Rule: rule.name}, true
		}
	}
	return Result{}, false
}

func (r compiledRule) matches(email, domain, subject string) bool {
	if len(r.emails) > 0 {
		if _, ok := r.emails[email]; !ok {
			return false
		}
	}
	if len(r.domains) > 0 {
		matched := false
		for _, suffix := range r.domains {
			if domainMatches(domain, suffix) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(r.keywords) > 0 {
		matched := false
		for _, keyword := range r.keywords {
			if strings.Contains(subject, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// domainMatches 做标签边界上的后缀匹配：github.com 命中 mail.github.com，
// 但不会命中 evilgithub.com。
func domainMatches(domain, suffix string) bool {
	return domain == suffix || strings.HasSuffix(domain, "."+suffix)
}

type parsedAddress struct {
	address string // 小写邮箱地址
	domain  string // 小写域名
}

// parseAddress 解析 From 头，兼容 "显示名 <a@b.com>" 与裸地址两种形式。
func parseAddress(raw string) (parsedAddress, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return parsedAddress{}, errors.New("空地址")
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return parsedAddress{}, fmt.Errorf("解析地址失败: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(addr.Address))
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return parsedAddress{}, fmt.Errorf("地址缺少有效的本地部分或域名: %q", addr.Address)
	}
	return parsedAddress{address: email, domain: email[at+1:]}, nil
}
