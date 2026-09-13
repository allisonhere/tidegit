// Package git provides cancellable repository operations backed by Git.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const outputLimit = 4 << 20

type Result struct {
	Stdout, Stderr string
	ExitCode       int
	Truncated      bool
}

type CommandError struct {
	Args   []string
	Result Result
	Cause  error
}

func (e *CommandError) Error() string {
	detail := strings.TrimSpace(e.Result.Stderr)
	if strings.TrimSpace(e.Result.Stdout) != "" {
		if detail != "" {
			detail += "\n"
		}
		detail += strings.TrimSpace(e.Result.Stdout)
	}
	if detail == "" {
		detail = e.Cause.Error()
	}
	if e.Result.Truncated {
		detail += "\n[Git output truncated at 4 MiB]"
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), detail)
}
func (e *CommandError) Unwrap() error { return e.Cause }

type boundedBuffer struct {
	buffer    bytes.Buffer
	truncated bool
}

// Do not embed bytes.Buffer: its promoted ReadFrom lets io.Copy bypass Write's limit.
func (b *boundedBuffer) Len() int       { return b.buffer.Len() }
func (b *boundedBuffer) String() string { return b.buffer.String() }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := outputLimit - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// newCommand builds a Git invocation. literalPathspecs is on for everything
// that touches repository paths; the one exception is `stash push -u`, whose
// include-untracked behaviour is implemented through Git's own pathspec magic
// and is disabled by the flag.
func newCommand(ctx context.Context, dir string, literalPathspecs bool, args ...string) *exec.Cmd {
	prefix := []string{"--no-pager"}
	if literalPathspecs {
		prefix = append(prefix, "--literal-pathspecs")
	}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	return cmd
}

func finish(ctx context.Context, cmd *exec.Cmd, args []string, out, errout *boundedBuffer, err error) (Result, error) {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	r := Result{Stdout: out.String(), Stderr: errout.String(), Truncated: out.truncated || errout.truncated}
	if err != nil {
		r.ExitCode = -1
		if cmd.ProcessState != nil {
			r.ExitCode = cmd.ProcessState.ExitCode()
		}
		return r, &CommandError{Args: args, Result: r, Cause: err}
	}
	return r, nil
}

func run(ctx context.Context, dir string, args ...string) (Result, error) {
	return runInput(ctx, dir, "", args...)
}

func runInput(ctx context.Context, dir, input string, args ...string) (Result, error) {
	cmd := newCommand(ctx, dir, true, args...)
	cmd.Stdin = strings.NewReader(input)
	var out, errout boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := cmd.Run()
	return finish(ctx, cmd, args, &out, &errout, err)
}

// runNoLiteral runs Git without --literal-pathspecs. Only the stash paths that
// depend on Git's pathspec magic use it.
func runNoLiteral(ctx context.Context, dir string, args ...string) (Result, error) {
	cmd := newCommand(ctx, dir, false, args...)
	cmd.Stdin = strings.NewReader("")
	var out, errout boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := cmd.Run()
	return finish(ctx, cmd, args, &out, &errout, err)
}

// runEnv runs Git with extra environment variables appended to the standard
// environment. Operation continuation uses it to pin GIT_EDITOR so a prepared
// message is accepted instead of opening an editor TideGit cannot show.
func runEnv(ctx context.Context, dir string, env []string, args ...string) (Result, error) {
	cmd := newCommand(ctx, dir, true, args...)
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader("")
	var out, errout boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := cmd.Run()
	return finish(ctx, cmd, args, &out, &errout, err)
}

// ProgressFunc receives the chunks Git writes to stderr while a long-running
// command runs. Chunks arrive in the order Git wrote them and may contain
// carriage returns, which Git uses to redraw a progress line in place.
type ProgressFunc func(chunk string)

// runStream runs Git like run, but also reports Git's stderr as it arrives so a
// network operation can show progress instead of freezing behind a silent
// pipe. stderr is still captured in full for error reporting. extraEnv is
// appended after the standard environment; it is how network operations turn
// off Git's terminal credential prompt.
func runStream(ctx context.Context, dir string, extraEnv []string, progress ProgressFunc, args ...string) (Result, error) {
	cmd := newCommand(ctx, dir, true, args...)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdin = strings.NewReader("")
	var out, errout boundedBuffer
	cmd.Stdout = &out
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, &CommandError{Args: args, Cause: err}
	}
	if err := cmd.Start(); err != nil {
		return Result{}, &CommandError{Args: args, Cause: err}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := pipe.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				_, _ = errout.Write(buf[:n])
				if progress != nil {
					progress(chunk)
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	wg.Wait()
	return finish(ctx, cmd, args, &out, &errout, waitErr)
}
