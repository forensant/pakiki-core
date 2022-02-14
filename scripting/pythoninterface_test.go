package scripting

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	_ "dev.forensant.com/pipeline/razor/proximitycore/internal/testing_init"
)

func TestRunScript(t *testing.T) {
	scripts := []struct {
		name   string
		code   string
		output string
	}{
		{"Hello world", "print('hello')", "hello"},
	}

	for _, script := range scripts {
		t.Run(script.name, func(t *testing.T) {
			guid := uuid.NewString()
			in, out, err := startPythonInterpreter(guid)

			if err != nil {
				t.Fatalf("Error starting python interpreter: %s", err)
			}

			err = sendCodeToInterpreter("script.py", script.code, in, out, true)

			if err != nil {
				t.Fatalf("Error sending script: %s", err)
			}
			in.Write([]byte("\nPROXIMITY_PYTHON_INTERPRETER_END_OF_SCRIPT\n"))

			scriptOutput, err := readFromBuffer(out)
			if err != nil {
				t.Errorf("Error reading script output: %s", err)
			}

			scriptOutput = strings.TrimSpace(string(stripOutputTags([]byte(scriptOutput))))

			if scriptOutput != script.output {
				t.Errorf("Expected output '%s' but got '%s'", script.output, scriptOutput)
			}
		})
	}
}
