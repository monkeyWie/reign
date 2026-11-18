package system

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestConsoleChildFixture(t *testing.T) {
	mode := os.Getenv("REIGN_CONSOLE_FIXTURE")
	if mode == "" {
		return
	}
	window, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	if window != 0 {
		os.Exit(2)
	}
	fmt.Println("ready")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		fmt.Println(scanner.Text())
		fmt.Fprint(os.Stderr, "stderr preserved")
	}
	os.Exit(0)
}

func TestHiddenConsolePreservesIOAndCancellation(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConsoleChildFixture$")
	cmd.Env = append(os.Environ(), "REIGN_CONSOLE_FIXTURE=echo")
	cmd.Stdin = strings.NewReader("hello\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil || string(out) != "ready\nhello\n" || stderr.String() != "stderr preserved" {
		t.Fatalf("hidden child: %q %q %v", out, stderr.String(), err)
	}

	life, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd = exec.CommandContext(life, os.Args[0], "-test.run=^TestConsoleChildFixture$")
	cmd.Env = append(os.Environ(), "REIGN_CONSOLE_FIXTURE=wait")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	HideConsole(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancellation did not terminate child")
	}
	if ctx.Err() != nil {
		t.Fatal("child required outer deadline to terminate")
	}
}
