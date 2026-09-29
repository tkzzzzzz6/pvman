//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// replaceRunning swaps newBin in for target, which may be the currently
// running executable. Windows refuses to touch the image file of a live
// process, so the swap is handed to a small detached helper that waits for
// this process to exit first, then moves the staged binary into place.
func replaceRunning(newBin, target string, out io.Writer) error {
	staged := target + ".new"
	data, err := os.ReadFile(newBin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(staged, data, 0o644); err != nil {
		return err
	}

	// Wait-Process returns immediately if we are already gone, so a slow
	// helper start is harmless; -Timeout bounds how long it will wait.
	ps := fmt.Sprintf(
		`$ErrorActionPreference='SilentlyContinue'; Wait-Process -Id %d -Timeout 120; Move-Item -Force %q %q; if (Test-Path %q) { Remove-Item -Force %q }`,
		os.Getpid(), staged, target, staged, staged)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn replace helper: %w", err)
	}

	targetName := filepath.Base(target)
	fmt.Fprintf(out, "update will finish in a moment (%s.new -> %s)\n", targetName, targetName)
	fmt.Fprintln(out, "close this window; the new version runs on the next launch")
	return nil
}
