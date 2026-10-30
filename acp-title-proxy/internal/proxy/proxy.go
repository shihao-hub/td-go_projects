package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync"

	"language_projects/go_projects/acp-title-proxy/internal/pool"
	"language_projects/go_projects/acp-title-proxy/internal/protocol"
	"language_projects/go_projects/acp-title-proxy/internal/skill"
)

type Titler interface {
	OnSessionNew(sessionID string)
	OnPrompt(sessionID string, prompt string)
}

type SkillManager interface {
	GetCommandItems() []protocol.CommandItem
	MatchAndRewrite(rawPrompt string) (bool, string, string, *skill.Skill)
}

type Runner struct {
	ExePath  string
	Args     []string
	Titler   Titler
	SkillMgr SkillManager
	Stdin    io.Reader
}

func (r *Runner) getStdin() io.Reader {
	if r.Stdin != nil {
		return r.Stdin
	}
	return os.Stdin
}

func NewRunner(exePath string, args []string, titler Titler, skillMgr SkillManager) *Runner {
	return &Runner{
		ExePath:  exePath,
		Args:     args,
		Titler:   titler,
		SkillMgr: skillMgr,
	}
}

func (r *Runner) Run(inj *Injector) error {
	cmd := exec.Command(r.ExePath, r.Args...)
	cmd.Env = os.Environ()

	childStdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer childStdin.Close()

	childStdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer childStdout.Close()

	childStderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	defer childStderr.Close()

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{})
	var closeOnce sync.Once
	abort := func() {
		closeOnce.Do(func() {
			close(done)
			_ = childStdin.Close()
			if cmd.Process != nil {
				_ = pool.KillProcessTree(cmd.Process.Pid)
				_ = cmd.Process.Kill()
			}
		})
	}
	defer abort()

	var wg sync.WaitGroup
	wg.Add(2)

	// 1. 直接转发 Stderr
	go func() {
		defer wg.Done()
		_, _ = io.Copy(os.Stderr, childStderr)
	}()

	// 2. 入向：Zed Stdin -> 子进程 Stdin
	// 不纳入 WaitGroup：客户端不关闭 stdin 时该 goroutine 会永久阻塞在 Read 上，
	// 若等待它，代理进程在会话重建后会残留为僵尸进程。
	go func() {
		defer abort() // Zed Stdin 关闭 -> 立即终止子进程

		reader := bufio.NewReaderSize(r.getStdin(), 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processInbound(line)
				if _, writeErr := childStdin.Write(newLine); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 3. 出向：子进程 Stdout -> Zed Stdout（经 Injector）
	go func() {
		defer wg.Done()
		defer abort()

		reader := bufio.NewReaderSize(childStdout, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processOutbound(line, inj)
				if _, writeErr := inj.WriteRaw(newLine); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	}

	abort()
	err = cmd.Wait()
	wg.Wait()
	return err
}

// RunStream 打通 Zed stdio 与已有的双工流（如已连接预热池）。
func (r *Runner) RunStream(stream io.ReadWriteCloser, inj *Injector) error {
	defer stream.Close()

	done := make(chan struct{})
	var closeOnce sync.Once
	abort := func() {
		closeOnce.Do(func() {
			close(done)
			_ = stream.Close()
		})
	}
	defer abort()

	var wg sync.WaitGroup
	wg.Add(1)

	// 1. 入向：Zed Stdin -> 双工流
	// 不纳入 WaitGroup：客户端不关闭 stdin 时该 goroutine 会永久阻塞在 Read 上，
	// 若等待它，代理进程在会话重建后会残留为僵尸进程。
	go func() {
		defer abort() // Zed Stdin 关闭 -> 关闭双工流并中止

		reader := bufio.NewReaderSize(r.getStdin(), 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processInbound(line)
				if _, writeErr := stream.Write(newLine); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 2. 出向：双工流 -> Zed Stdout（经 Injector）
	go func() {
		defer wg.Done()
		defer abort()

		reader := bufio.NewReaderSize(stream, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processOutbound(line, inj)
				if _, writeErr := inj.WriteRaw(newLine); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	}

	abort()
	wg.Wait()
	return nil
}

func (r *Runner) processInbound(line []byte) []byte {
	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return line
	}

	if msg.Method == "session/prompt" && len(msg.Params) > 0 {
		var params protocol.SessionPromptParams
		if err := json.Unmarshal(msg.Params, &params); err == nil && params.SessionID != "" {
			promptText := protocol.ExtractPromptText(params.Prompt)
			effectivePrompt := promptText

			// 检查技能匹配并重写 prompt
			if r.SkillMgr != nil && promptText != "" {
				matched, rewritten, task, _ := r.SkillMgr.MatchAndRewrite(promptText)
				if matched {
					// 1. 向 Titler 传入干净的用户任务，保证会话标题有意义
					if r.Titler != nil {
						titlerPrompt := task
						if titlerPrompt == "" {
							titlerPrompt = promptText
						}
						r.Titler.OnPrompt(params.SessionID, titlerPrompt)
					}

					// 2. 注入技能并重写 prompt
					if newPromptRaw, err := protocol.RewritePrompt(params.Prompt, rewritten); err == nil {
						params.Prompt = newPromptRaw
						if newParamsRaw, err := json.Marshal(params); err == nil {
							msg.Params = newParamsRaw
							if newLine, err := json.Marshal(msg); err == nil {
								newLine = append(newLine, '\n')
								return newLine
							}
						}
					}
					return line
				}
			}

			// 未匹配到技能时，按常规通知 Titler
			if r.Titler != nil && effectivePrompt != "" {
				r.Titler.OnPrompt(params.SessionID, effectivePrompt)
			}
		}
	}

	return line
}

func (r *Runner) processOutbound(line []byte, inj *Injector) []byte {
	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return line
	}

	// 1. 识别 session/new 响应：{"id": 1, "result": {"sessionId": "...", "availableCommands": [...]}}
	if msg.ID != nil && len(msg.Result) > 0 {
		var res protocol.SessionNewResult
		if err := json.Unmarshal(msg.Result, &res); err == nil && res.SessionID != "" {
			if r.Titler != nil {
				r.Titler.OnSessionNew(res.SessionID)
			}

			// 将官方思维命令与自定义技能合并进 availableCommands，并过滤裸 skill
			var skills []protocol.CommandItem
			if r.SkillMgr != nil {
				skills = r.SkillMgr.GetCommandItems()
			}
			if mergedResult, err := protocol.MergeAvailableCommands(msg.Result, skills); err == nil {
				msg.Result = mergedResult
				if newLine, err := json.Marshal(msg); err == nil {
					newLine = append(newLine, '\n')
					return newLine
				}
			}
		}
	}

	// 2. 识别 session/update 通知：available_commands_update
	if msg.Method == "session/update" && len(msg.Params) > 0 {
		var skills []protocol.CommandItem
		if r.SkillMgr != nil {
			skills = r.SkillMgr.GetCommandItems()
		}
		if mergedParams, matched, err := protocol.MergeCommandsUpdateNotification(msg.Params, skills); err == nil && matched {
			msg.Params = mergedParams
			if newLine, err := json.Marshal(msg); err == nil {
				newLine = append(newLine, '\n')
				return newLine
			}
		}
	}

	return line
}
