// Package helperproc provides a supervised, reusable subprocess with a
// newline-delimited request/response protocol. A canceled or hung operation
// destroys the helper so the next operation starts on a clean process.
package helperproc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

var ErrClosed = errors.New("helper process manager closed")

type Manager struct {
	gate       chan struct{}
	command    func() *exec.Cmd
	proc       *exec.Cmd
	input      io.WriteCloser
	output     *bufio.Reader
	uses       int
	closed     bool
	maxUses    int
	maxRequest int
}

func New(command func() *exec.Cmd, maxUses int) *Manager {
	if maxUses < 1 {
		maxUses = 48
	}
	m := &Manager{maxRequest: 8192, gate: make(chan struct{}, 1), command: command, maxUses: maxUses}
	m.gate <- struct{}{}
	return m
}

// NewWithRequestLimit allows a bounded in-memory image payload for local OCR.
func NewWithRequestLimit(command func() *exec.Cmd, maxUses, maxRequest int) *Manager {
	m := New(command, maxUses)
	if maxRequest > 8192 && maxRequest <= 512*1024 {
		m.maxRequest = maxRequest
	}
	return m
}

func (m *Manager) lock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.gate:
		return nil
	}
}
func (m *Manager) unlock() { m.gate <- struct{}{} }

func (m *Manager) start() error {
	cmd := m.command()
	input, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("helper stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return fmt.Errorf("helper stdout: %w", err)
	}
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		_ = stdout.Close()
		return fmt.Errorf("helper start: %w", err)
	}
	m.proc, m.input, m.output, m.uses = cmd, input, bufio.NewReaderSize(stdout, 8192), 0
	return nil
}

// kill is called with gate held and unblocks any pending pipe readers.
func (m *Manager) kill() {
	if m.proc == nil {
		return
	}
	_ = m.input.Close()
	if m.proc.Process != nil {
		_ = m.proc.Process.Kill()
	}
	_ = m.proc.Wait()
	m.proc, m.input, m.output, m.uses = nil, nil, nil, 0
}

type reply struct {
	text string
	err  error
}

// Request consumes one request and at most one newline-terminated response.
// It is context-cancelable including while another request owns the helper.
// Any protocol, IO or timeout error causes a fresh child on the next call.
func (m *Manager) Request(ctx context.Context, line string) (string, error) {
	if len(line) == 0 || len(line) > m.maxRequest || strings.ContainsAny(line, "\r\n") {
		return "", errors.New("invalid helper request")
	}
	if err := m.lock(ctx); err != nil {
		return "", err
	}
	defer m.unlock()
	if m.closed {
		return "", ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if m.proc != nil && m.uses >= m.maxUses {
		m.kill()
	}
	if m.proc == nil {
		if err := m.start(); err != nil {
			return "", err
		}
	}
	// A tiny request fits inside an OS pipe buffer. Still make it cancelable
	// in case the child stops consuming stdin.
	sent := make(chan error, 1)
	input := m.input
	go func() { _, e := io.WriteString(input, line+"\n"); sent <- e }()
	select {
	case <-ctx.Done():
		m.kill()
		return "", ctx.Err()
	case err := <-sent:
		if err != nil {
			m.kill()
			return "", err
		}
	}
	read := make(chan reply, 1)
	output := m.output
	go func() {
		value, err := output.ReadString('\n')
		if len(value) > 16384 {
			err = errors.New("helper response too large")
		}
		read <- reply{strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r"), err}
	}()
	select {
	case <-ctx.Done():
		m.kill()
		return "", ctx.Err()
	case r := <-read:
		if err := ctx.Err(); err != nil {
			m.kill()
			return "", err
		}
		if r.err != nil {
			m.kill()
			return "", r.err
		}
		m.uses++
		return r.text, nil
	}
}

// Close is safe while another request is running; it cancels nothing itself.
// The caller should cancel outstanding operations before calling Close.
func (m *Manager) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if m.lock(ctx) != nil {
		return
	}
	defer m.unlock()
	m.closed = true
	m.kill()
}

// Reset discards a helper that has become unhealthy even if it returned a
// syntactically valid response (e.g. COM initialization failed). The next
// request starts a new helper process.
func (m *Manager) Reset(ctx context.Context) error {
	if err := m.lock(ctx); err != nil {
		return err
	}
	defer m.unlock()
	if m.closed {
		return ErrClosed
	}
	m.kill()
	return nil
}
