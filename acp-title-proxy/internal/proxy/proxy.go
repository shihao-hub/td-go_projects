package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
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

type sessionWorkflowState struct {
	mode             string // "normal" or "plan"
	awaitingApproval bool   // 方案已生成，正等待用户审批放行
	lastPlanReqID    string // 上一轮触发 plan 的请求 ID
}

type Runner struct {
	ExePath       string
	Args          []string
	Titler        Titler
	SkillMgr      SkillManager
	Stdin         io.Reader
	workflowMu    sync.Mutex
	sessionStates map[string]*sessionWorkflowState
}

func (r *Runner) getStdin() io.Reader {
	if r.Stdin != nil {
		return r.Stdin
	}
	return os.Stdin
}

func (r *Runner) getWorkflowState(sessionID string) *sessionWorkflowState {
	r.workflowMu.Lock()
	defer r.workflowMu.Unlock()
	if r.sessionStates == nil {
		r.sessionStates = make(map[string]*sessionWorkflowState)
	}
	s, ok := r.sessionStates[sessionID]
	if !ok {
		s = &sessionWorkflowState{
			mode: "normal",
		}
		r.sessionStates[sessionID] = s
	}
	return s
}

func NewRunner(exePath string, args []string, titler Titler, skillMgr SkillManager) *Runner {
	return &Runner{
		ExePath:       exePath,
		Args:          args,
		Titler:        titler,
		SkillMgr:      skillMgr,
		sessionStates: make(map[string]*sessionWorkflowState),
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

	// 1. 直接透传 Stderr
	go func() {
		defer wg.Done()
		_, _ = io.Copy(os.Stderr, childStderr)
	}()

	// 2. Inbound: Zed Stdin -> Child Stdin
	// 不纳入 WaitGroup：客户端不关闭 stdin 时该 goroutine 会永久阻塞在 Read 上，
	// 若等待它，代理进程在会话重建后会残留为僵尸进程。
	go func() {
		defer abort()

		reader := bufio.NewReaderSize(r.getStdin(), 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processInbound(line, inj)
				if len(newLine) > 0 {
					if _, writeErr := childStdin.Write(newLine); writeErr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 3. Outbound: Child Stdout -> Zed Stdout (via Injector)
	go func() {
		defer wg.Done()
		defer abort()

		reader := bufio.NewReaderSize(childStdout, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processOutbound(line, inj)
				if len(newLine) > 0 {
					if _, writeErr := inj.WriteRaw(newLine); writeErr != nil {
						return
					}
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

// RunStream bridges Zed stdio with an existing duplex stream (e.g. connected to pre-warm pool).
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

	// 1. Inbound: Zed Stdin -> Stream
	// 不纳入 WaitGroup：客户端不关闭 stdin 时该 goroutine 会永久阻塞在 Read 上，
	// 若等待它，代理进程在会话重建后会残留为僵尸进程。
	go func() {
		defer abort()

		reader := bufio.NewReaderSize(r.getStdin(), 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processInbound(line, inj)
				if len(newLine) > 0 {
					if _, writeErr := stream.Write(newLine); writeErr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 2. Outbound: Stream -> Zed Stdout (via Injector)
	go func() {
		defer wg.Done()
		defer abort()

		reader := bufio.NewReaderSize(stream, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				newLine := r.processOutbound(line, inj)
				if len(newLine) > 0 {
					if _, writeErr := inj.WriteRaw(newLine); writeErr != nil {
						return
					}
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

func (r *Runner) processInbound(line []byte, inj *Injector) []byte {
	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return line
	}

	// 1. 拦截 Zed 切换下拉框配置 (session/set_config_option)
	if msg.Method == "session/set_config_option" && len(msg.Params) > 0 {
		var setParams protocol.SetConfigOptionParams
		if err := json.Unmarshal(msg.Params, &setParams); err == nil && setParams.SessionID != "" {
			if strings.EqualFold(setParams.ConfigID, "workflow") {
				state := r.getWorkflowState(setParams.SessionID)
				r.workflowMu.Lock()
				state.mode = strings.ToLower(setParams.Value)
				state.awaitingApproval = false // 切换模式时重置等待状态
				r.workflowMu.Unlock()

				// 向 Zed 假装回复成功，拦截不发往底层 child
				if inj != nil && msg.ID != nil {
					resp := map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      msg.ID,
						"result":  map[string]interface{}{},
					}
					if respData, err := json.Marshal(resp); err == nil {
						respData = append(respData, '\n')
						_, _ = inj.WriteRaw(respData)
					}
				}
				return nil
			}
		}
	}

	// 2. 拦截与重写 session/prompt
	if msg.Method == "session/prompt" && len(msg.Params) > 0 {
		var params protocol.SessionPromptParams
		if err := json.Unmarshal(msg.Params, &params); err == nil && params.SessionID != "" {
			promptText := protocol.ExtractPromptText(params.Prompt)
			effectivePrompt := promptText
			userTask := promptText

			state := r.getWorkflowState(params.SessionID)
			r.workflowMu.Lock()
			currentMode := state.mode
			isAwaitingApproval := state.awaitingApproval
			r.workflowMu.Unlock()

			// A. 技能匹配与注入
			skillMatched := false
			if r.SkillMgr != nil && promptText != "" {
				matched, rewritten, task, _ := r.SkillMgr.MatchAndRewrite(promptText)
				if matched {
					skillMatched = true
					effectivePrompt = rewritten
					userTask = task
				}
			}

			// B. Plan 模式与生命周期状态机处理
			shouldInjectPlan := false
			if currentMode == "plan" {
				if isAwaitingApproval {
					// 方案 A 核心点：处于等审批状态，说明用户正在反馈或批准刚才的方案！
					// 坚决不注入 /plan，直接放行让其进入执行期！
					r.workflowMu.Lock()
					state.awaitingApproval = false
					r.workflowMu.Unlock()
				} else {
					// 首次提需求，若未显式带有 /plan 则自动戴上 /plan 帽子
					trimmedEffective := strings.TrimSpace(effectivePrompt)
					if !strings.HasPrefix(trimmedEffective, "/plan") {
						shouldInjectPlan = true
					}
				}
			}

			if shouldInjectPlan {
				effectivePrompt = "/plan " + effectivePrompt
				if msg.ID != nil {
					r.workflowMu.Lock()
					state.lastPlanReqID = string(*msg.ID)
					r.workflowMu.Unlock()
				}
			} else {
				// 如果用户自己手敲了 /plan，也记录一下，以便监听其 plan 完成
				if strings.HasPrefix(strings.TrimSpace(promptText), "/plan") && msg.ID != nil {
					r.workflowMu.Lock()
					state.lastPlanReqID = string(*msg.ID)
					r.workflowMu.Unlock()
				}
			}

			// C. 更新 Titler（使用干净的用户任务描述，不带 /plan 与技能模版前缀）
			if r.Titler != nil && userTask != "" {
				cleanTitlePrompt := strings.TrimPrefix(strings.TrimSpace(userTask), "/plan")
				cleanTitlePrompt = strings.TrimSpace(cleanTitlePrompt)
				if cleanTitlePrompt != "" {
					r.Titler.OnPrompt(params.SessionID, cleanTitlePrompt)
				}
			}

			// D. 如果发生改写（技能或 /plan 注入），替换并重组 JSON-RPC
			if skillMatched || shouldInjectPlan {
				if newPromptRaw, err := protocol.RewritePrompt(params.Prompt, effectivePrompt); err == nil {
					params.Prompt = newPromptRaw
					if newParamsRaw, err := json.Marshal(params); err == nil {
						msg.Params = newParamsRaw
						if newLine, err := json.Marshal(msg); err == nil {
							newLine = append(newLine, '\n')
							return newLine
						}
					}
				}
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

	// 1. 识别 session/new 响应：合并 availableCommands 与注入 workflow 独立下拉框
	if msg.ID != nil && len(msg.Result) > 0 {
		var res protocol.SessionNewResult
		if err := json.Unmarshal(msg.Result, &res); err == nil && res.SessionID != "" {
			if r.Titler != nil {
				r.Titler.OnSessionNew(res.SessionID)
			}

			// A. 合并命令列表（包含官方解封命令 + 本地技能族）
			var skills []protocol.CommandItem
			if r.SkillMgr != nil {
				skills = r.SkillMgr.GetCommandItems()
			}
			if mergedCmds, err := protocol.MergeAvailableCommands(msg.Result, skills); err == nil {
				msg.Result = mergedCmds
			}

			// B. 注入独立下拉框 (Workflow: Normal / Plan)
			if mergedOpts, err := protocol.MergeConfigOptions(msg.Result, protocol.DefaultWorkflowConfigOption); err == nil {
				msg.Result = mergedOpts
			}

			if newLine, err := json.Marshal(msg); err == nil {
				newLine = append(newLine, '\n')
				return newLine
			}
		}
	}

	// 2. 识别 session/update 通知：合并 available_commands_update
	if msg.Method == "session/update" && len(msg.Params) > 0 {
		if r.SkillMgr != nil {
			skills := r.SkillMgr.GetCommandItems()
			if len(skills) > 0 {
				if mergedParams, matched, err := protocol.MergeCommandsUpdateNotification(msg.Params, skills); err == nil && matched {
					msg.Params = mergedParams
					if newLine, err := json.Marshal(msg); err == nil {
						newLine = append(newLine, '\n')
						return newLine
					}
				}
			}
		}
	}

	// 3. 监听 prompt 请求完成：进入 awaitingApproval 状态
	if msg.ID != nil && len(msg.Result) > 0 {
		idStr := string(*msg.ID)
		r.workflowMu.Lock()
		for _, st := range r.sessionStates {
			if st.lastPlanReqID != "" && st.lastPlanReqID == idStr {
				st.awaitingApproval = true // 规划输出完毕，正式进入等待用户审批状态！
				st.lastPlanReqID = ""
				break
			}
		}
		r.workflowMu.Unlock()
	}

	return line
}
