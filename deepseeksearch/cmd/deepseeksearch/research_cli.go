//go:build cli

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"deepseeksearch/internal/app"
	"deepseeksearch/internal/research"
	"deepseeksearch/internal/storage"
)

func researchCLI(args []string) int {
	usage := func() {
		fmt.Println("用法: research <create|list|show|suggest|set-queries|run|feedback|schema> [选项]\n选项在前：--id ID --question 问题 --file 草稿.json --kind 类型 --text 反馈 --result 结果ID\n通用：--json --schema --db 路径；run：--retry-failed --refresh；set-queries 文件字段：queries/count/needContent/localization/revision")
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		usage()
		return 0
	}
	command := args[0]
	fs := flag.NewFlagSet("research "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "研究 ID")
	question := fs.String("question", "", "原始问题")
	file := fs.String("file", "", "草稿 JSON 文件")
	kind := fs.String("kind", "custom", "反馈类型")
	text := fs.String("text", "", "反馈内容")
	result := fs.String("result", "", "反馈结果 ID")
	asJSON := fs.Bool("json", false, "JSON 包络")
	schema := fs.Bool("schema", false, "离线 Schema")
	db := fs.String("db", "", "数据库路径")
	retry := fs.Bool("retry-failed", false, "仅重试上轮失败查询")
	refresh := fs.Bool("refresh", false, "跳过缓存")
	emit := func(data interface{}, err error) int {
		out := research.Wrap(data, err)
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(out)
		} else if out.Error != nil {
			fmt.Fprintln(os.Stderr, out.Error.Message)
			if data != nil {
				printResearch(data)
			}
		} else {
			printResearch(data)
		}
		if out.Error != nil {
			if out.Error.Code == "invalid_argument" {
				return 2
			}
			return 1
		}
		return 0
	}
	// 即使参数解析失败也尊重显式 JSON 模式，不向 stdout 混入提示。
	for _, arg := range args[1:] {
		if arg == "--json" || arg == "--json=true" {
			*asJSON = true
		}
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage()
			return 0
		}
		if err == flag.ErrHelp {
			usage()
			return 0
		}
		return emit(nil, &research.Error{Code: "invalid_argument", Message: err.Error()})
	}
	if fs.NArg() != 0 {
		return emit(nil, &research.Error{Code: "invalid_argument", Message: "请使用命名选项提供参数"})
	}
	switch command {
	case "create", "list", "show", "suggest", "set-queries", "run", "feedback", "schema":
	default:
		return emit(nil, &research.Error{Code: "invalid_argument", Message: "未知 research 子命令"})
	}
	if command == "schema" || *schema {
		_ = json.NewEncoder(os.Stdout).Encode(research.Schema())
		return 0
	}
	allowed := map[string]bool{"json": true, "db": true, "schema": true}
	if command != "create" && command != "list" {
		allowed["id"] = true
	}
	switch command {
	case "create":
		allowed["question"] = true
	case "set-queries":
		allowed["file"] = true
	case "feedback":
		allowed["kind"] = true
		allowed["text"] = true
		allowed["result"] = true
	case "run":
		allowed["retry-failed"] = true
		allowed["refresh"] = true
	}
	var invalidFlag string
	fs.Visit(func(f *flag.Flag) {
		if !allowed[f.Name] {
			invalidFlag = f.Name
		}
	})
	if invalidFlag != "" {
		return emit(nil, &research.Error{Code: "invalid_argument", Message: "该命令不支持 --" + invalidFlag})
	}
	if command != "create" && command != "list" && *id == "" {
		return emit(nil, &research.Error{Code: "invalid_argument", Message: "缺少 --id"})
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	path := *db
	if path == "" {
		path = app.DefaultDBPath()
	}
	store, err := storage.NewStore(path)
	if err != nil {
		return emit(nil, err)
	}
	defer store.Close()
	service := research.New(store)
	switch command {
	case "create":
		v, e := service.Create(ctx, *question)
		return emit(v, e)
	case "list":
		v, e := service.List(ctx)
		return emit(v, e)
	case "show":
		v, e := service.Get(ctx, *id)
		return emit(v, e)
	case "suggest":
		v, e := service.Suggest(ctx, *id)
		return emit(v, e)
	case "feedback":
		v, e := service.AddFeedback(ctx, *id, research.Feedback{Kind: *kind, Text: *text, ResultID: *result})
		return emit(v, e)
	case "set-queries":
		if *file == "" {
			return emit(nil, &research.Error{Code: "invalid_argument", Message: "缺少 --file"})
		}
		f, e := os.Open(*file)
		if e != nil {
			return emit(nil, &research.Error{Code: "invalid_argument", Message: "无法读取草稿文件"})
		}
		defer f.Close()
		dec := json.NewDecoder(io.LimitReader(f, 128<<10))
		dec.DisallowUnknownFields()
		var draft research.Draft
		if e := dec.Decode(&draft); e != nil {
			return emit(nil, &research.Error{Code: "invalid_argument", Message: "草稿 JSON 无效"})
		}
		var extra interface{}
		if dec.Decode(&extra) != io.EOF {
			return emit(nil, &research.Error{Code: "invalid_argument", Message: "草稿包含多余内容"})
		}
		v, e := service.SetDraft(ctx, *id, draft)
		return emit(v, e)
	case "run":
		fmt.Fprintln(os.Stderr, "执行选中的查询；进度已逐路保存，可通过 research show 查看。")
		v, e := service.Run(ctx, *id, research.RunOptions{RetryFailed: *retry, Refresh: *refresh}, func(v *research.Session) {
			if len(v.Rounds) == 0 {
				return
			}
			r := v.Rounds[len(v.Rounds)-1]
			completed := 0
			for _, e := range r.Executions {
				if e.Status == "success" || e.Status == "failed" {
					completed++
				}
			}
			fmt.Fprintf(os.Stderr, "第 %d 轮：%d/%d 路已完成\n", r.Number, completed, len(r.Executions))
		})
		return emit(v, e)
	}
	return 0
}

func printResearch(data interface{}) {
	switch v := data.(type) {
	case *research.Session:
		if v == nil {
			return
		}
		fmt.Printf("研究 %s\n问题：%s\n版本：%d　轮次：%d　结果：%d\n", v.ID, v.Question, v.Revision, len(v.Rounds), len(v.Results))
		for _, q := range v.Draft.Queries {
			mark := " "
			if q.Selected {
				mark = "x"
			}
			fmt.Printf("[%s] %s  %s\n    %s\n", mark, q.ID, q.Query, q.Reason)
		}
		for _, round := range v.Rounds {
			fmt.Printf("第 %d 轮：%s\n", round.Number, round.Status)
			for _, e := range round.Executions {
				fmt.Printf("  %s  %s  %s\n", e.Status, e.Query.Query, e.Error)
			}
		}
		for i, r := range v.Results {
			fmt.Printf("%d. %s\n   %s\n", i+1, r.Title, r.URL)
		}
	case []storage.ResearchDocument:
		for _, d := range v {
			fmt.Printf("%s  %s  %s\n", d.ID, d.UpdatedAt, d.Question)
		}
	}
}
