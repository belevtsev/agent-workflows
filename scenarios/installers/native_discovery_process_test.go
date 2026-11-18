package installers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const (
	inventoryProcessFixture = "installer-inventory-fixture"
	inventoryHelperReady    = "inventory helper ready\n"
)

func TestInventoryReapsHelperWithInheritedStreams(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "profile.lock")
	runInventoryProcess(t, proc.Spec{
		Path: exe, Args: []string{inventoryProcessFixture, "server", locked}, Timeout: 15 * time.Second,
	}, func(stdout io.Reader, stdin io.WriteCloser) {
		reader := bufio.NewReader(stdout)
		if ready, err := reader.ReadString('\n'); ready != inventoryHelperReady || err != nil {
			t.Fatalf("inventory helper did not take ownership of the streams: %q (%v)", ready, err)
		}
		if err := stdin.Close(); err != nil {
			t.Fatal(err)
		}
		if extra, err := io.ReadAll(reader); err != nil || len(extra) != 0 {
			t.Fatalf("expected EOF after the server and helper exited, got %q (%v)", extra, err)
		}
	})
	if err := os.Remove(locked); err != nil {
		t.Fatalf("remove profile file after inventory shutdown: %v", err)
	}
}

func inventoryFixture(role, locked string) error {
	if role == "helper" {
		file, err := os.Create(locked)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := io.WriteString(os.Stderr, "helper holds stderr\n"); err != nil {
			return err
		}
		if _, err := io.WriteString(os.Stdout, inventoryHelperReady); err != nil {
			return err
		}
		// A finite fallback avoids leaving the intentional orphan indefinitely
		// if the test ever runs it without process-group or job supervision.
		time.Sleep(20 * time.Second)
		_, err = io.WriteString(os.Stdout, "helper outlived the server\n")
		return err
	}
	if role != "server" {
		return fmt.Errorf("unknown inventory fixture role %q", role)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	helper := exec.Command(exe, inventoryProcessFixture, "helper", locked)
	helper.Stdout, helper.Stderr = os.Stdout, os.Stderr
	if err := helper.Start(); err != nil {
		return err
	}
	// Match the app-server's normal exit on EOF while leaving the helper active.
	_, err = io.Copy(io.Discard, os.Stdin)
	return errors.Join(err, helper.Process.Release())
}
