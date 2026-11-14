package parser

import (
	"strings"
	"testing"
)

func TestParseGitHubNewIssue(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: [octocat/hello-world] New issue: Something is broken (#42)",
		"List-Id: octocat/hello-world <hello-world.octocat.github.com>",
		"X-GitHub-Reason: subscribed",
		"X-GitHub-Sender: someuser",
		"Content-Type: text/plain; charset=utf-8",
	}, "Somebody opened a new issue.\n\nView it on GitHub:\nhttps://github.com/octocat/hello-world/issues/42\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	gh := mail.GitHub
	if gh == nil {
		t.Fatal("应识别为 GitHub 通知")
	}
	if gh.Repo != "octocat/hello-world" {
		t.Errorf("Repo = %q", gh.Repo)
	}
	if gh.Number != 42 {
		t.Errorf("Number = %d", gh.Number)
	}
	if gh.Kind != "issue" {
		t.Errorf("Kind = %q", gh.Kind)
	}
	if gh.Title != "Something is broken" {
		t.Errorf("Title = %q", gh.Title)
	}
	if gh.URL != "https://github.com/octocat/hello-world/issues/42" {
		t.Errorf("URL = %q", gh.URL)
	}
	if gh.Actor != "someuser" || gh.Reason != "subscribed" {
		t.Errorf("Actor/Reason = %q/%q", gh.Actor, gh.Reason)
	}
}

func TestParseGitHubCommentReply(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: Re: [octocat/hello-world] Something is broken (#42)",
		"X-GitHub-Reason: mention",
		"X-GitHub-Sender: anotheruser",
		"Content-Type: text/plain; charset=utf-8",
	}, "A new comment.\n\nhttps://github.com/octocat/hello-world/issues/42#issuecomment-1\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	gh := mail.GitHub
	if gh == nil {
		t.Fatal("应识别为 GitHub 通知")
	}
	if gh.Number != 42 || gh.Title != "Something is broken" {
		t.Errorf("Number/Title = %d/%q", gh.Number, gh.Title)
	}
	if gh.Kind != "issue" {
		t.Errorf("Kind = %q", gh.Kind)
	}
}

func TestParseGitHubPullRequest(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: [owner/repo] Add feature (#7)",
		"List-Id: owner/repo <repo.owner.github.com>",
		"X-GitHub-Reason: review_requested",
		"Content-Type: text/plain; charset=utf-8",
	}, "Review requested.\nhttps://github.com/owner/repo/pull/7\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	gh := mail.GitHub
	if gh == nil {
		t.Fatal("应识别为 GitHub 通知")
	}
	if gh.Kind != "pull" {
		t.Errorf("Kind = %q, want pull", gh.Kind)
	}
	if gh.URL != "https://github.com/owner/repo/pull/7" {
		t.Errorf("URL = %q", gh.URL)
	}
	if gh.Title != "Add feature" {
		t.Errorf("Title = %q", gh.Title)
	}
}

func TestParseGitHubFallsBackToConstructedURL(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: [owner/repo] Title (#9)",
		"X-GitHub-Reason: author",
		"Content-Type: text/plain; charset=utf-8",
	}, "正文没有链接\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	gh := mail.GitHub
	if gh == nil {
		t.Fatal("应识别为 GitHub 通知")
	}
	if gh.URL != "https://github.com/owner/repo/issues/9" {
		t.Errorf("URL = %q", gh.URL)
	}
}

func TestParseGitHubPrefersMatchingURL(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: [owner/repo] Title (#9)",
		"List-Id: owner/repo <repo.owner.github.com>",
		"X-GitHub-Reason: subscribed",
		"Content-Type: text/plain; charset=utf-8",
	}, "引用了另一个 issue https://github.com/other/project/issues/1\n"+
		"目标 https://github.com/owner/repo/issues/9\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	gh := mail.GitHub
	if gh == nil {
		t.Fatal("应识别为 GitHub 通知")
	}
	if gh.URL != "https://github.com/owner/repo/issues/9" {
		t.Errorf("URL = %q, 应优先匹配 repo 与编号", gh.URL)
	}
}

func TestParseNonGitHubMailIsNotGitHub(t *testing.T) {
	raw := rawMail(t, []string{
		"From: Boss <boss@example.com>",
		"Subject: [owner/repo] looks like github but is not",
		"Content-Type: text/plain; charset=utf-8",
	}, "普通邮件\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if mail.GitHub != nil {
		t.Errorf("非 GitHub 发件人不应识别为 GitHub 通知: %+v", mail.GitHub)
	}
}

func TestParseGitHubWithGitHubListIDOnly(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: [owner/repo] Title (#3)",
		"Content-Type: text/plain; charset=utf-8",
	}, "https://github.com/owner/repo/issues/3\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if mail.GitHub == nil || mail.GitHub.Repo != "owner/repo" {
		t.Fatalf("GitHub 解析结果 = %+v", mail.GitHub)
	}
	if !strings.HasPrefix(mail.GitHub.URL, "https://github.com/owner/repo/issues/") {
		t.Errorf("URL = %q", mail.GitHub.URL)
	}
}
