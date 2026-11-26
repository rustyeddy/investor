package portfolio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BranchPrefix starts the name of an experiment's Portfolio branch.
const BranchPrefix = "research/"

// Branch is the one branch experiment id is archived on.
func Branch(id string) string { return BranchPrefix + id }

// checkWorktree refuses to write anywhere but the experiment's own worktree: dir
// must be the top of a linked worktree of the repository checked out at main,
// never main's own checkout, with the experiment's branch checked out.
func checkWorktree(main, dir, id string) error {
	top, err := gitLine(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if !samePath(top, dir) {
		return fmt.Errorf("%s is not the top of a worktree (that is %s)", dir, top)
	}
	own, err := gitLine(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	common, err := gitLine(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	if samePath(own, common) {
		return fmt.Errorf("%s is a main checkout; archive writes only into a linked worktree", dir)
	}
	mainCommon, err := gitLine(main, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	if !samePath(common, mainCommon) {
		return fmt.Errorf("%s is not a worktree of the Portfolio repository at %s", dir, main)
	}
	branch, err := gitLine(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	if branch != Branch(id) {
		return fmt.Errorf("%s has branch %q checked out; experiment %s is archived only on %q", dir, branch, id, Branch(id))
	}
	return nil
}

// gitLine runs git in dir and returns its one line of output. Variables that
// would point git at another repository are removed.
func gitLine(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s %s: %w: %s", dir, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func samePath(a, b string) bool {
	x, errA := os.Stat(filepath.Clean(a))
	y, errB := os.Stat(filepath.Clean(b))
	return errA == nil && errB == nil && os.SameFile(x, y)
}
