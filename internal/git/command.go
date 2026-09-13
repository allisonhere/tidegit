// Package git provides read-only, cancellable repository operations backed by Git.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func run(ctx context.Context, dir string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "--literal-pathspecs"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var out, errout boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := cmd.Run()
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
