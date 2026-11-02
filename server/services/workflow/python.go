package workflow

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowfunction "github.com/JieWaZi/NeuxBot/server/internal/workflow/function"
	"os/exec"
	"time"
)

// resolvePython 为能力发现和运行接纳使用同一解释器及版本校验。
func (s *Service) resolvePython(ctx context.Context) (string, error) {
	executable := s.pythonExecutable
	if executable == "" {
		executable = "python3"
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return "", status_error.WorkflowResourceInvalid.WithField("field", "pythonExecutable")
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := workflowfunction.PythonVersion(check, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}
