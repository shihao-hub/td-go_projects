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
	wg.Add(3)

	// 1. Forward Stderr directly
	go func() {
		defer wg.Done()
		_, _ = io.Copy(os.Stderr, childStderr)
	}()

	// 2. Inbound: Zed Stdin -> Child Stdin
	go func() {
		defer wg.Done()
		defer abort() // Zed Stdin closed -> terminate child immediately

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

	// 3. Outbound: Child Stdout -> Zed Stdout (via Injector)
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
	wg.Add(2)

	// 1. Inbound: Zed Stdin -> Stream
	go func() {
		defer wg.Done()
		defer abort() // Zed Stdin closed -> close stream and abort

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

	// 2. Outbound: Stream -> Zed Stdout (via Injector)
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

			// Check skill matching and prompt rewrite
			if r.SkillMgr != nil && promptText != "" {
				matched, rewritten, task, _ := r.SkillMgr.MatchAndRewrite(promptText)
				if matched {
					// 1. Pass clean user task to Titler so session title is meaningful
					if r.Titler != nil {
						titlerPrompt := task
						if titlerPrompt == "" {
							titlerPrompt = promptText
						}
						r.Titler.OnPrompt(params.SessionID, titlerPrompt)
					}

					// 2. Rewrite prompt with skill injection
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

			// If no skill matched, notify Titler normally
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

	// 1. Detect session/new response: {"id": 1, "result": {"sessionId": "...", "availableCommands": [...]}}
	if msg.ID != nil && len(msg.Result) > 0 {
		var res protocol.SessionNewResult
		if err := json.Unmarshal(msg.Result, &res); err == nil && res.SessionID != "" {
			if r.Titler != nil {
				r.Titler.OnSessionNew(res.SessionID)
			}

			// Merge custom skills into availableCommands
			if r.SkillMgr != nil {
				skills := r.SkillMgr.GetCommandItems()
				if len(skills) > 0 {
					if mergedResult, err := protocol.MergeAvailableCommands(msg.Result, skills); err == nil {
						msg.Result = mergedResult
						if newLine, err := json.Marshal(msg); err == nil {
							newLine = append(newLine, '\n')
							return newLine
						}
					}
				}
			}
		}
	}

	// 2. Detect session/update notification: available_commands_update
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

	return line
}
