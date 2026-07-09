package enhancer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const longOutputLineSize = 2 * 1024 * 1024

func TestRunPreparedCommandHandlesLongOutputLine(t *testing.T) {
	t.Parallel()

	cmd := exec.Command(os.Args[0], "-test.run=TestCommandOutputHelper")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=long-line")

	gotLength := 0
	err := runPreparedCommand(context.Background(), commandHooks{
		Line: func(line string) {
			if strings.HasPrefix(line, "$ ") {
				return
			}
			gotLength = len(line)
		},
	}, cmd)
	if err != nil {
		t.Fatal(err)
	}

	if gotLength != longOutputLineSize {
		t.Fatalf("output line length = %d, want %d", gotLength, longOutputLineSize)
	}
}

func TestCommandOutputHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "long-line" {
		return
	}

	fmt.Print(strings.Repeat("x", longOutputLineSize))
	os.Exit(0)
}
