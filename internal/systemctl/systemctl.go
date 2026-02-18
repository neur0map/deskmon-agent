package systemctl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

const serviceName = "deskmon-agent"

// ErrDockerMode is returned when agent control is attempted inside a Docker container.
var ErrDockerMode = errors.New("agent control not available in Docker mode — use docker restart instead")

// isDockerMode returns true when running inside a Docker container.
func isDockerMode() bool {
	if os.Getenv("DESKMON_HOST_ROOT") != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

func Restart() error {
	if isDockerMode() {
		return ErrDockerMode
	}
	cmd := exec.Command("systemctl", "restart", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl restart failed: %w", err)
	}
	return nil
}

func Stop() error {
	if isDockerMode() {
		return ErrDockerMode
	}
	cmd := exec.Command("systemctl", "stop", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl stop failed: %w", err)
	}
	return nil
}

func Status() (string, error) {
	if isDockerMode() {
		return "running (docker)", nil
	}
	cmd := exec.Command("systemctl", "is-active", serviceName)
	out, err := cmd.Output()
	if err != nil {
		// is-active returns exit code 3 for inactive, which is not an error for us
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(exitErr.Stderr), nil
		}
		return "unknown", nil
	}
	return string(out), nil
}
