//go:build !windows

package update

import (
	"fmt"
	"io"
	"os"
)

// replaceRunning swaps newBin in for target, which may be the currently
// running executable. On Unix a running process keeps its old inode, so
// renaming over the path is safe mid-flight; the new binary takes over on
// the next launch.
func replaceRunning(newBin, target string, out io.Writer) error {
	if err := os.Chmod(newBin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(newBin, target); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated %s -- restart pvman to run the new version\n", target)
	return nil
}
