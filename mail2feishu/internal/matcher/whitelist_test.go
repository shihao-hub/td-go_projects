package matcher

import "testing"

func mustMatcher(t *testing.T, rules ...Rule) *Matcher {
	t.Helper()
	m, err := New(rules)
	if err != nil {
		t.Fatalf("New() 返回错误: %v", err)
	}
	return m
}

func TestMatchExactEmailWithDisplayName(t *testing.T) {
	m := mustMatcher(t, Rule{
		Name:         "github",
		SenderEmails: []string{"notifications@github.com"},
	})
	result, ok := m.Match("GitHub <notifications@github.com>", "[repo] New issue")
	if !ok {
		t.Fatal("精确邮箱应命中")
	}
	if result.Rule != "github" {
		t.Fatalf("命中规则名 = %q, want github", result.Rule)
	}
}

func TestMatchEmailIsCaseInsensitive(t *testing.T) {
	m := mustMatcher(t, Rule{
		Name:         "vip",
		SenderEmails: []string{"Boss@Example.COM"},
	})
	if _, ok := m.Match("boss@example.com", "hello"); !ok {
		t.Fatal("邮箱匹配应忽略大小写")
	}
}

func TestMatchDomainSuffix(t *testing.T) {
	m := mustMatcher(t, Rule{
		Name:          "github",
		SenderDomains: []string{"github.com"},
	})
	for _, from := range []string{
		"notifications@github.com",
		"notifications@mail.github.com",
		"GitHub <NOTIFICATIONS@GitHub.COM>",
	} {
		if _, ok := m.Match(from, "subject"); !ok {
			t.Fatalf("%q 应命中域名后缀", from)
		}
	}
}

func TestMatchDomainDoesNotCrossLabelBoundary(t *testing.T) {
	m := mustMatcher(t, Rule{
		Name:          "github",
		SenderDomains: []string{"github.com"},
	})
	for _, from := range []string{
		"attacker@evilgithub.com",
		"attacker@github.com.evil.test",
	} {
		if _, ok := m.Match(from, "subject"); ok {
			t.Fatalf("%q 不应命中 github.com", from)
		}
	}
}

func TestMatchSubjectKeywordCaseInsensitive(t *testing.T) {
	m := mustMatcher(t, Rule{
		Name:            "urgent",
		SubjectKeywords: []string{"URGENT"},
	})
	if _, ok := m.Match("a@b.com", "please review: urgent request"); !ok {
		t.Fatal("关键词匹配应忽略大小写并支持子串")
	}
}

func TestRuleConditionsAreAND(t *testing.T) {
	m := mustMatcher(t, Rule{
		Name:            "github-issue",
		SenderDomains:   []string{"github.com"},
		SubjectKeywords: []string{"issue"},
	})
	if _, ok := m.Match("notifications@github.com", "[repo] New issue"); !ok {
		t.Fatal("域名与关键词同时满足时应命中")
	}
	if _, ok := m.Match("notifications@github.com", "[repo] New pull request"); ok {
		t.Fatal("仅满足域名、不满足关键词时不应命中")
	}
	if _, ok := m.Match("someone@example.com", "issue"); ok {
		t.Fatal("仅满足关键词、不满足域名时不应命中")
	}
}

func TestRuleValuesWithinCategoryAreOR(t *testing.T) {
	t.Run("emails", func(t *testing.T) {
		m := mustMatcher(t, Rule{
			Name:         "emails",
			SenderEmails: []string{"a@example.com", "b@example.com"},
		})
		for _, from := range []string{"a@example.com", "b@example.com"} {
			if _, ok := m.Match(from, "subject"); !ok {
				t.Fatalf("%q 应命中邮箱 OR 条件", from)
			}
		}
	})

	t.Run("domains", func(t *testing.T) {
		m := mustMatcher(t, Rule{
			Name:          "domains",
			SenderDomains: []string{"example.com", "github.com"},
		})
		for _, from := range []string{"notify@github.com", "notify@example.com"} {
			if _, ok := m.Match(from, "subject"); !ok {
				t.Fatalf("%q 应命中域名 OR 条件", from)
			}
		}
	})

	t.Run("keywords", func(t *testing.T) {
		m := mustMatcher(t, Rule{
			Name:            "keywords",
			SubjectKeywords: []string{"issue", "bug"},
		})
		for _, subject := range []string{"new issue", "a bug report"} {
			if _, ok := m.Match("notify@example.com", subject); !ok {
				t.Fatalf("%q 应命中关键词 OR 条件", subject)
			}
		}
	})
}

func TestRulesAreOR(t *testing.T) {
	m := mustMatcher(t,
		Rule{Name: "github", SenderDomains: []string{"github.com"}},
		Rule{Name: "vip", SenderEmails: []string{"boss@example.com"}},
	)
	if result, ok := m.Match("boss@example.com", "hi"); !ok || result.Rule != "vip" {
		t.Fatalf("应命中 vip 规则, got %+v ok=%v", result, ok)
	}
	if result, ok := m.Match("notifications@github.com", "hi"); !ok || result.Rule != "github" {
		t.Fatalf("应命中 github 规则, got %+v ok=%v", result, ok)
	}
}

func TestEmptyMatcherBlocksEverything(t *testing.T) {
	m := mustMatcher(t)
	if _, ok := m.Match("notifications@github.com", "anything"); ok {
		t.Fatal("空白名单必须阻断所有邮件")
	}
}

func TestUnparsableFromIsBlocked(t *testing.T) {
	m := mustMatcher(t, Rule{Name: "all-domains", SenderDomains: []string{"example.com"}})
	for _, from := range []string{"", "   ", "not-an-address", "a@", "@b.com"} {
		if _, ok := m.Match(from, "subject"); ok {
			t.Fatalf("无法解析的 From %q 必须阻断", from)
		}
	}
}

func TestNewRejectsEmptyRule(t *testing.T) {
	if _, err := New([]Rule{{Name: "broken"}}); err == nil {
		t.Fatal("空规则应报错，避免匹配一切")
	}
}

func TestNewRejectsInvalidRuleValues(t *testing.T) {
	cases := []Rule{
		{Name: "bad-email", SenderEmails: []string{"not-an-email"}},
		{Name: "bad-domain", SenderDomains: []string{"@github.com"}},
		{Name: "blank-domain", SenderDomains: []string{"  "}},
		{Name: "blank-keyword", SubjectKeywords: []string{" "}},
	}
	for _, rule := range cases {
		if _, err := New([]Rule{rule}); err == nil {
			t.Fatalf("规则 %q 应报错", rule.Name)
		}
	}
}
