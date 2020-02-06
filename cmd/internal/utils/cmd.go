package utils

import (
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

type CmdExecutor struct {
	log *zap.SugaredLogger
}

func NewExecutor(log *zap.SugaredLogger) *CmdExecutor {
	return &CmdExecutor{
		log: log,
	}
}

func (c *CmdExecutor) ExecuteCommandWithOutput(command string, env []string, arg ...string) (string, error) {
	c.log.Infow("running command", "command", command, "args", strings.Join(arg, " "))
	cmd := exec.Command(command, arg...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, env...)
	return runCommandWithOutput(cmd, true)
}

func runCommandWithOutput(cmd *exec.Cmd, combinedOutput bool) (string, error) {
	var output []byte
	var err error

	if combinedOutput {
		output, err = cmd.CombinedOutput()
	} else {
		output, err = cmd.Output()
	}

	out := strings.TrimSpace(string(output))

	return out, err
}
