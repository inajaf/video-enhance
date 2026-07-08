package enhancer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
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
	read := func(reader io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		for scanner.Scan() {
			if hooks.Line != nil {
				hooks.Line(scanner.Text())
			}
		}
	}
	wg.Add(2)
	go read(stdout)
	go read(stderr)

	done := make(chan error, 1)
	go func() {
		wg.Wait()
		done <- cmd.Wait()
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
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '/' && r != '.' && r != '-' && r != '_' && r != ':' && r != '=' && r != ',' && r != '+' && r != '%' {
			return fmt.Sprintf("%q", value)
		}
	}
	return value
}
