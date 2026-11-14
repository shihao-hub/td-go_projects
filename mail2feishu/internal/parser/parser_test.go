package parser

import (
	"strings"
	"testing"
)

// rawMail 把头部行与正文拼成 CRLF 邮件原文。
func rawMail(t *testing.T, headers []string, body string) []byte {
	t.Helper()
	body = strings.ReplaceAll(body, "\n", "\r\n")
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

func TestParsePlainTextMail(t *testing.T) {
	raw := rawMail(t, []string{
		"From: =?UTF-8?B?5byg5LiJ?= <zhangsan@example.com>",
		"To: me@qq.com",
		"Subject: =?UTF-8?B?5rWL6K+V6YKu5Lu2?=",
		"Date: Mon, 02 Jan 2006 15:04:05 -0700",
		"Message-ID: <abc@example.com>",
		"Content-Type: text/plain; charset=utf-8",
	}, "第一行\n第二行\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if mail.FromAddress != "zhangsan@example.com" {
		t.Errorf("FromAddress = %q", mail.FromAddress)
	}
	if mail.Subject != "测试邮件" {
		t.Errorf("Subject = %q, want 测试邮件", mail.Subject)
	}
	if !strings.Contains(mail.Text, "第一行") || !strings.Contains(mail.Text, "第二行") {
		t.Errorf("Text = %q", mail.Text)
	}
	if mail.Date.Year() != 2006 {
		t.Errorf("Date = %v", mail.Date)
	}
	if mail.MessageID != "abc@example.com" {
		t.Errorf("MessageID = %q", mail.MessageID)
	}
	if mail.GitHub != nil {
		t.Error("普通邮件不应被识别为 GitHub 通知")
	}
}

func TestParseMultipartAlternativePrefersPlainText(t *testing.T) {
	raw := rawMail(t, []string{
		"From: a@example.com",
		"Subject: multipart",
		`Content-Type: multipart/alternative; boundary="b"`,
	}, strings.Join([]string{
		"--b",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"=E4=BD=A0=E5=A5=BD=EF=BC=8C=E8=BF=99=E6=98=AF=E6=AD=A3=E6=96=87=E3=80=82",
		"--b",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>HTML 版本不应优先</p>",
		"--b--",
		"",
	}, "\n"))

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if mail.Text != "你好，这是正文。" {
		t.Errorf("Text = %q", mail.Text)
	}
}

func TestParseGBKBody(t *testing.T) {
	raw := rawMail(t, []string{
		"From: a@example.com",
		"Subject: gbk",
		"Content-Type: text/plain; charset=gbk",
		"Content-Transfer-Encoding: base64",
	}, "xOO6ww==\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if !strings.Contains(mail.Text, "你好") {
		t.Errorf("GBK 正文未正确解码: %q", mail.Text)
	}
}

func TestParseHTMLOnlyMail(t *testing.T) {
	raw := rawMail(t, []string{
		"From: a@example.com",
		"Subject: html",
		"Content-Type: text/html; charset=utf-8",
	}, `<html><body><p>Hello <b>world</b></p><script>alert(1)</script></body></html>`)

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if !strings.Contains(mail.Text, "Hello world") {
		t.Errorf("HTML 正文未转为文本: %q", mail.Text)
	}
	if strings.Contains(mail.Text, "alert") {
		t.Errorf("script 内容不应保留: %q", mail.Text)
	}
}

func TestParseStripsGitHubFooter(t *testing.T) {
	raw := rawMail(t, []string{
		"From: GitHub <notifications@github.com>",
		"Subject: [o/r] Title (#1)",
		"X-GitHub-Reason: subscribed",
		"Content-Type: text/plain; charset=utf-8",
	}, "正文内容\n\n-- \nYou are receiving this because you are subscribed to this thread.\nReply to this email directly, view it on GitHub:\nhttps://github.com/o/r/issues/1\n")

	mail, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() 错误: %v", err)
	}
	if strings.Contains(mail.Text, "You are receiving this because") {
		t.Errorf("GitHub 尾部样板未被移除: %q", mail.Text)
	}
	if !strings.Contains(mail.Text, "正文内容") {
		t.Errorf("正文丢失: %q", mail.Text)
	}
}

func TestParseRejectsEmptyInput(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("空输入应报错")
	}
}

func TestSummary(t *testing.T) {
	text := "一二三四五六七八九十"
	if got := Summary(text, 5); got != "一二三四五…" {
		t.Errorf("Summary() = %q", got)
	}
	if got := Summary(text, 20); got != text {
		t.Errorf("未超长时不应截断: %q", got)
	}
	if got := Summary("  x  ", 10); got != "x" {
		t.Errorf("Summary() 应去除首尾空白: %q", got)
	}
}

func TestCleanTextCollapsesBlankLines(t *testing.T) {
	got := cleanText("a\n\n\n\nb\r\nc\r\n")
	want := "a\n\nb\nc"
	if got != want {
		t.Errorf("cleanText() = %q, want %q", got, want)
	}
}
