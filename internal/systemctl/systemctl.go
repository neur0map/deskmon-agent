package systemctl

import (
	"fmt"
	"os/exec"
)

const serviceName = "deskmon-agent"

func Restart() error {
	cmd := exec.Command("systemctl", "restart", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl restart failed: %w", err)
	}
	return nil
}

func Stop() error {
	cmd := exec.Command("systemctl", "stop", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl stop failed: %w", err)
	}
	return nil
}

func Status() (string, error) {
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
