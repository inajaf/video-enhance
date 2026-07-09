package enhancer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type commandHooks struct {
	Line func(string)
	Tick func()
}

func runCommand(ctx context.Context, hooks commandHooks, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return runPreparedCommand(ctx, hooks, cmd)
}

func runPreparedCommand(ctx context.Context, hooks commandHooks, cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if hooks.Line != nil {
		hooks.Line("$ " + printableCommand(cmd))
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	readErrs := make(chan error, 2)
	read := func(reader io.Reader) {
		defer wg.Done()
		readErrs <- readCommandOutput(reader, hooks.Line)
	}
	wg.Add(2)
	go read(stdout)
	go read(stderr)

	done := make(chan error, 1)
	go func() {
		wg.Wait()
		close(readErrs)

		waitErr := cmd.Wait()
		for err := range readErrs {
			waitErr = errors.Join(waitErr, err)
		}

		done <- waitErr
	}()

	var ticker *time.Ticker
	if hooks.Tick != nil {
		ticker = time.NewTicker(time.Second)
		defer ticker.Stop()
	}

	for {
		select {
		case err := <-done:
			if hooks.Tick != nil {
				hooks.Tick()
			}
			return err
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-done
			return ctx.Err()
		case <-tickC(ticker):
			hooks.Tick()
		}
	}
}

func readCommandOutput(reader io.Reader, lineHook func(string)) error {
	buffered := bufio.NewReader(reader)

	for {
		line, err := buffered.ReadString('\n')
		if line != "" && lineHook != nil {
			lineHook(strings.TrimRight(line, "\r\n"))
		}

		if err == nil {
			continue
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		return err
	}
}

func tickC(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func printableCommand(cmd *exec.Cmd) string {
	out := cmd.Path
	for _, arg := range cmd.Args[1:] {
		out += " " + shellQuote(arg)
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	for _, r := range value {
		if !isShellSafe(r) {
			return fmt.Sprintf("%q", value)
		}
	}
	return value
}

func isShellSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '/', r == '.', r == '-', r == '_', r == ':', r == '=', r == ',', r == '+', r == '%':
		return true
	default:
		return false
	}
}
