package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
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

// PlanModeInstruction 是处于 Plan 模式时强制注入的安全手刹约束，模拟 OpenCode 的严格规划与切回提示。
const PlanModeInstruction = "【系统约束：当前处于 Plan 规划模式（只读分析阶段）。严禁调用任何具有修改文件、创建文件、写入代码或产生系统副作用的工具；仅允许使用只读工具（如读取文件、搜索代码）进行分析，并输出严谨的实施方案。若用户要求直接动手执行或编写代码，请务必明确提示用户：“当前处于 Plan 规划模式，请先在底部状态栏将 Workflow 切换为 Normal 后，我再立即为您执行代码修改。”】\n\n"

type sessionWorkflowState struct {
	mode              string // "normal" or "plan"
	lastConfigOptions []protocol.ConfigOption
}

type Runner struct {
	ExePath                string
	Args                   []string
	Titler                 Titler
	SkillMgr               SkillManager
	Stdin                  io.Reader
	workflowMu             sync.Mutex
	sessionStates          map[string]*sessionWorkflowState
	lastKnownConfigOptions []protocol.ConfigOption
}

func (r *Runner) buildUpdatedConfigOptions(sessionID, newWorkflowValue string) []protocol.ConfigOption {
	r.workflowMu.Lock()
	defer r.workflowMu.Unlock()

	var baseOpts []protocol.ConfigOption
	if st, ok := r.sessionStates[sessionID]; ok && len(st.lastConfigOptions) > 0 {
		baseOpts = make([]protocol.ConfigOption, len(st.lastConfigOptions))
		copy(baseOpts, st.lastConfigOptions)
	} else if len(r.lastKnownConfigOptions) > 0 {
		baseOpts = make([]protocol.ConfigOption, len(r.lastKnownConfigOptions))
		copy(baseOpts, r.lastKnownConfigOptions)
	}

	baseOpts = protocol.RewriteModeConfigOption(baseOpts, newWorkflowValue)

	// 缓存回状态
	if st, ok := r.sessionStates[sessionID]; ok {
		st.lastConfigOptions = baseOpts
	}
	r.lastKnownConfigOptions = baseOpts

	return baseOpts
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
	if msg.Method != "" {
		log.Printf("[Proxy IN] method=%s id=%v", msg.Method, msg.ID)
	}

	// 1. 拦截 Zed 切换 Mode (session/set_mode，由 Shift+Tab 快捷键或点击 Mode 下拉框触发)
	if msg.Method == "session/set_mode" && len(msg.Params) > 0 {
		var setModeParams protocol.SetModeParams
		if err := json.Unmarshal(msg.Params, &setModeParams); err == nil && setModeParams.SessionID != "" {
			val := strings.ToLower(strings.TrimSpace(setModeParams.ModeID))
			mappedMode := "normal"
			if val == "plan" {
				mappedMode = "plan"
			}

			state := r.getWorkflowState(setModeParams.SessionID)
			r.workflowMu.Lock()
			state.mode = mappedMode
			r.workflowMu.Unlock()

			// 向 Zed 直接回复成功结果，拦截不透传给底层（底层保持 YOLO）
			if inj != nil && msg.ID != nil {
				resp := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      msg.ID,
					"result":  map[string]interface{}{},
				}
				if respData, err := json.Marshal(resp); err == nil {
					respData = append(respData, '\n')
					_, _ = inj.WriteRaw(respData)
					log.Printf("[Proxy IN] Intercepted set_mode=%s (mapped=%s) for session=%s", val, mappedMode, setModeParams.SessionID)
				}
			}
			return nil
		}
	}

	// 2. 兼容拦截 Zed 切换下拉框配置 (session/set_config_option)
	if msg.Method == "session/set_config_option" && len(msg.Params) > 0 {
		var setParams protocol.SetConfigOptionParams
		if err := json.Unmarshal(msg.Params, &setParams); err == nil && setParams.SessionID != "" {
			if strings.EqualFold(setParams.ConfigID, "workflow") || strings.EqualFold(setParams.ConfigID, "mode") {
				val := strings.ToLower(strings.TrimSpace(setParams.Value))
				mappedMode := "normal"
				if val == "plan" {
					mappedMode = "plan"
				}

				state := r.getWorkflowState(setParams.SessionID)
				r.workflowMu.Lock()
				state.mode = mappedMode
				r.workflowMu.Unlock()

				updatedOpts := r.buildUpdatedConfigOptions(setParams.SessionID, mappedMode)

				// 向 Zed 回复包含全量 configOptions 的成功结果，确保客户端状态更新与 UI 联动
				if inj != nil && msg.ID != nil {
					resp := map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      msg.ID,
						"result": map[string]interface{}{
							"configOptions": updatedOpts,
						},
					}
					if respData, err := json.Marshal(resp); err == nil {
						respData = append(respData, '\n')
						_, _ = inj.WriteRaw(respData)
						log.Printf("[Proxy IN] Responded to set_config_option %s=%s (mapped=%s)", setParams.ConfigID, val, mappedMode)
					}
				}
				return nil
			}
		}
	}

	// 感知 session/load 请求，为恢复的历史会话预先初始化状态
	if msg.Method == "session/load" && len(msg.Params) > 0 {
		var loadParams struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(msg.Params, &loadParams); err == nil && loadParams.SessionID != "" {
			_ = r.getWorkflowState(loadParams.SessionID)
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

			// B. Plan 模式：OpenCode 安全手刹机制
			planInjected := false
			if currentMode == "plan" {
				planInjected = true
				trimmed := strings.TrimSpace(effectivePrompt)
				userContent := strings.TrimPrefix(trimmed, "/plan")
				userContent = strings.TrimSpace(userContent)
				effectivePrompt = "/plan " + PlanModeInstruction + userContent
			}

			// C. 更新 Titler（使用干净的用户任务描述，不带 /plan 与技能模版前缀）
			if r.Titler != nil && userTask != "" {
				cleanTitlePrompt := strings.TrimPrefix(strings.TrimSpace(userTask), "/plan")
				cleanTitlePrompt = strings.TrimSpace(cleanTitlePrompt)
				if cleanTitlePrompt != "" {
					r.Titler.OnPrompt(params.SessionID, cleanTitlePrompt)
				}
			}

			// D. 如果发生改写（技能或 Plan 手刹注入），替换并重组 JSON-RPC
			if skillMatched || planInjected {
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
	log.Printf("[Proxy OUT] %s", string(line[:min(len(line), 250)]))
	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return line
	}

	// 1. 识别 session/new 与 session/load 响应：合并 availableCommands 与注入 workflow 独立下拉框
	if msg.ID != nil && len(msg.Result) > 0 {
		var resMap map[string]json.RawMessage
		if err := json.Unmarshal(msg.Result, &resMap); err == nil {
			modified := false

			// 如果有 sessionId，触发 Titler
			if rawSID, ok := resMap["sessionId"]; ok {
				var sid string
				if err := json.Unmarshal(rawSID, &sid); err == nil && sid != "" && r.Titler != nil {
					r.Titler.OnSessionNew(sid)
				}
			}

			// 如果包含 availableCommands，合并官方思维命令与技能族
			if _, ok := resMap["availableCommands"]; ok {
				var skills []protocol.CommandItem
				if r.SkillMgr != nil {
					skills = r.SkillMgr.GetCommandItems()
				}
				if mergedCmds, err := protocol.MergeAvailableCommands(msg.Result, skills); err == nil {
					msg.Result = mergedCmds
					modified = true
				}
			}

			_, hasSID := resMap["sessionId"]
			_, hasModes := resMap["modes"]
			_, hasOpts := resMap["configOptions"]

			var targetSID string
			if rawSID, ok := resMap["sessionId"]; ok {
				_ = json.Unmarshal(rawSID, &targetSID)
			}
			currentMode := "normal"
			if targetSID != "" {
				st := r.getWorkflowState(targetSID)
				r.workflowMu.Lock()
				if st.mode != "" {
					currentMode = st.mode
				}
				r.workflowMu.Unlock()
			}

			// 2. 将 modes 替换为 Workflow 模式（Normal / Plan），实现旧版 ACP 兼容
			if hasModes || hasSID {
				if replaced, err := protocol.ReplaceModesInResult(msg.Result, currentMode); err == nil {
					msg.Result = replaced
					modified = true
				}
			}

			// 3. 将 configOptions 中的 mode 替换为 Normal / Plan 模式，实现新版 Zed Shift+Tab 循环
			if hasOpts || hasSID {
				if rewrittenOpts, err := protocol.RewriteConfigOptionsInResult(msg.Result, currentMode); err == nil {
					msg.Result = rewrittenOpts
					modified = true

					// 记录全量 configOptions
					var allOpts struct {
						ConfigOptions []protocol.ConfigOption `json:"configOptions"`
					}
					if err := json.Unmarshal(rewrittenOpts, &allOpts); err == nil && len(allOpts.ConfigOptions) > 0 {
						r.workflowMu.Lock()
						r.lastKnownConfigOptions = allOpts.ConfigOptions
						if targetSID != "" {
							if st, ok := r.sessionStates[targetSID]; ok {
								st.lastConfigOptions = allOpts.ConfigOptions
							}
						}
						r.workflowMu.Unlock()
					}
				}
			}

			if modified {
				if newLine, err := json.Marshal(msg); err == nil {
					newLine = append(newLine, '\n')
					log.Printf("[Proxy OUT] Injected workflow/commands into response id=%v", msg.ID)
					return newLine
				}
			}
		}
	}

	// 2. 识别 session/update 通知：合并 availableCommands (session/load 会推送此通知)
	if msg.Method == "session/update" && len(msg.Params) > 0 {
		var skills []protocol.CommandItem
		if r.SkillMgr != nil {
			skills = r.SkillMgr.GetCommandItems()
		}
		if mergedParams, matched, err := protocol.MergeCommandsUpdateNotification(msg.Params, skills); err == nil && matched {
			msg.Params = mergedParams
			if newLine, err := json.Marshal(msg); err == nil {
				newLine = append(newLine, '\n')
				log.Printf("[Proxy OUT] Injected skills/commands into session/update notification")
				return newLine
			}
		}
	}

	return line
}
