package utils

import (
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

func (c *CmdExecutor) ExecuteCommandWithOutput(command string, arg ...string) (string, error) {
	c.log.Infow("running command", "command", command, "args", strings.Join(arg, " "))
	cmd := exec.Command(command, arg...)
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
