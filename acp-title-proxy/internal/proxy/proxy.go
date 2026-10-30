package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync"

	"language_projects/go_projects/acp-title-proxy/internal/protocol"
)

type Titler interface {
	OnSessionNew(sessionID string)
	OnPrompt(sessionID string, prompt string)
}

type Runner struct {
	ExePath string
	Args    []string
	Titler  Titler
}

func NewRunner(exePath string, args []string, titler Titler) *Runner {
	return &Runner{
		ExePath: exePath,
		Args:    args,
		Titler:  titler,
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
		defer childStdin.Close()

		reader := bufio.NewReaderSize(os.Stdin, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				r.inspectInbound(line)
				if _, writeErr := childStdin.Write(line); writeErr != nil {
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

		reader := bufio.NewReaderSize(childStdout, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				r.inspectOutbound(line)
				if _, writeErr := inj.WriteRaw(line); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	err = cmd.Wait()
	wg.Wait()
	return err
}

// RunStream bridges Zed stdio with an existing duplex stream (e.g. connected to pre-warm pool).
func (r *Runner) RunStream(stream io.ReadWriteCloser, inj *Injector) error {
	defer stream.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// 1. Inbound: Zed Stdin -> Stream
	go func() {
		defer wg.Done()
		defer func() {
			if cw, ok := stream.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
		}()

		reader := bufio.NewReaderSize(os.Stdin, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				r.inspectInbound(line)
				if _, writeErr := stream.Write(line); writeErr != nil {
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

		reader := bufio.NewReaderSize(stream, 1024*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				r.inspectOutbound(line)
				if _, writeErr := inj.WriteRaw(line); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

func (r *Runner) inspectInbound(line []byte) {
	if r.Titler == nil {
		return
	}

	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}

	if msg.Method == "session/prompt" && len(msg.Params) > 0 {
		var params protocol.SessionPromptParams
		if err := json.Unmarshal(msg.Params, &params); err == nil && params.SessionID != "" {
			promptText := protocol.ExtractPromptText(params.Prompt)
			if promptText != "" {
				r.Titler.OnPrompt(params.SessionID, promptText)
			}
		}
	}
}

func (r *Runner) inspectOutbound(line []byte) {
	if r.Titler == nil {
		return
	}

	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}

	// Detect session/new response: {"id": 1, "result": {"sessionId": "..."}}
	if msg.ID != nil && len(msg.Result) > 0 {
		var res protocol.SessionNewResult
		if err := json.Unmarshal(msg.Result, &res); err == nil && res.SessionID != "" {
			r.Titler.OnSessionNew(res.SessionID)
		}
	}
}
