package cli

import (
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func onboardingPTY(t *testing.T) *os.File {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Skipf("pseudo terminal unavailable: %v", err)
	}
	master := os.NewFile(uintptr(fd), "onboarding-pty")
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, master)
		close(done)
	}()
	t.Cleanup(func() {
		_ = master.Close()
		<-done
	})
	return slave
}

func TestOnboardingInteractiveStartPreservesChildExitTwo(t *testing.T) {
	_, capture := onboardingReadyFixture(t)
	t.Setenv("KITSU_TEST_ONBOARDING_TTY", "1")
	t.Setenv("KITSU_TEST_ONBOARDING_EXIT", "2")
	terminal := onboardingPTY(t)
	err := Execute(t.Context(), []string{"task", "start", "A literal request", "--host", "codex"}, Config{In: terminal, Out: terminal, Err: terminal})
	if ExitCode(err) != 2 || err.Error() != "" {
		t.Fatalf("host's exit 2 was rewritten as command usage: %v", err)
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), "A literal request") {
		t.Fatalf("interactive start did not pass the request: %s, %v", data, err)
	}
}
