package docker

import (
	"fmt"
	"os/exec"
	"strings"
)

// RemoveContainersForWorktree stops and removes any docker containers whose
// docker-compose project working directory matches worktreePath. Containers
// started via `docker compose up` carry a com.docker.compose.project.working_dir
// label that we filter on; non-compose containers are unaffected.
//
// Returns nil if no matching containers exist or if docker is unavailable.
func RemoveContainersForWorktree(worktreePath string) error {
	out, err := exec.Command("docker", "ps", "-aq",
		"--filter", fmt.Sprintf("label=com.docker.compose.project.working_dir=%s", worktreePath),
	).Output()
	if err != nil {
		return nil
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	return exec.Command("docker", args...).Run()
}
