package scripting

// Design decisions: We're spawning a separate Python process to get around the
// Python Global Interpreter Lock (GIL). You can do multi-threaded code by embeding it in Go, but
// with the GIL there is a possibility of bugs if you're not concious of what variables you're updating and when.
// It was decided that it'd be easier to have each Python script run independently of each other in their own process.

// Python was chosen because a lot of people in the infosec community already are familiar with it, and it has a great
// standard library. It does have the tradeoff that it can be a pain to get the environment nicely working when
// deploying the application.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "embed"

	"github.com/google/uuid"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"dev.forensant.com/pipeline/razor/proximitycore/proxy/request_queue"
)

//go:embed common.py
var commonCode string

var runningScripts = make(map[string]*exec.Cmd)

type ScriptCaller interface {
	RecordError(string)
}

func CancelScriptInternal(guid string) error {
	command, ok := runningScripts[guid]

	if ok {
		return command.Process.Kill()
	}

	project.CancelScript(guid)

	return nil
}

func readString(delimeter byte, r *bufio.Reader) (string, error) {
	for {
		str, err := r.ReadString(delimeter)
		if err == nil {
			return str, nil
		} else if err != io.EOF {
			return "", err
		} else {
			if str != "" {
				fmt.Printf("EOF reached, string: %s\n", str)
			}
		}
	}
}

func recordInProject(guid string, script string, title string, development bool, output string, err string, status string) {
	scriptRun := project.ScriptRun{
		GUID:        guid,
		Script:      script,
		TextOutput:  output,
		Title:       title,
		Error:       err,
		Status:      status,
		Development: development,
	}
	scriptRun.RecordOrUpdate()
}

func StartScript(hostPort string, script string, title string, development bool, guid string, apiKey string, scriptCaller ScriptCaller) (string, error) {

	if guid == "" {
		guid = uuid.NewString()
	}

	executablePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	executablePath = filepath.Dir(executablePath)
	pythonPath := executablePath + "/pythoninterpreter"

	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		executablePath, err = os.Getwd()
		if err != nil {
			log.Println(err)
		}
		pythonPath = executablePath + "/pythoninterpreter"
	}

	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		return "", errors.New("could not find Python interpreter")
	}

	pythonCmd := exec.Command(pythonPath)
	pythonIn, err := pythonCmd.StdinPipe()
	if err != nil {
		return "", err
	}
	pythonOut, err := pythonCmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	bufferedOutput := bufio.NewReader(pythonOut)

	err = pythonCmd.Start()
	if err != nil {
		return "", err
	}
	runningScripts[guid] = pythonCmd

	go func() {
		_, err = pythonIn.Write([]byte(getCommonCode(guid, hostPort, apiKey)))
		if err != nil {
			fmt.Println("Error writing bytes to process: " + err.Error())
		}
		pythonIn.Write([]byte("\nPROXIMITY_PYTHON_INTERPRETER_END_OF_BLOCK\n"))

		output, err := readString('\n', bufferedOutput)
		if err != nil {
			err := "Error running script: " + err.Error()
			if scriptCaller != nil {
				scriptCaller.RecordError(err)
			} else {
				fmt.Println(err)
			}

			recordInProject(guid, script, title, development, "", err, "Error")

			pythonCmd.Process.Kill()
			delete(runningScripts, guid)

			return
		}

		if strings.TrimSpace(output) != "PROXIMITY_PYTHON_INTERPRETER_READY" {
			err := "Unexpected output running script: " + output
			if scriptCaller != nil {
				scriptCaller.RecordError(err)
			} else {
				fmt.Println(err)
			}

			recordInProject(guid, script, title, development, "", err, "Error")

			pythonCmd.Process.Kill()
			delete(runningScripts, guid)
			return
		}

		// do the initial record into the project
		recordInProject(guid, script, title, development, "", "", "Running")

		pythonIn.Write([]byte(script))
		pythonIn.Write([]byte("\nPROXIMITY_PYTHON_INTERPRETER_END_INTERPRETER\n"))

		go func() {
			readBuf := make([]byte, 1024)
			fullOutput := make([]byte, 0)
			for {
				bytesRead, err := pythonOut.Read(readBuf)
				lineRead := readBuf[:bytesRead]

				if bytesRead != 0 {
					fullOutput = stripOutputTags(append(fullOutput, lineRead...))
					outputUpdate := project.ScriptOutputUpdate{
						GUID:       guid,
						TextOutput: string(stripOutputTags(lineRead)),
					}
					outputUpdate.Record()
				}

				if err != nil {
					// will indicate that the file has been closed
					recordInProject(guid, script, title, development, string(fullOutput), "", "Completed")
					request_queue.CloseQueueIfEmpty(guid)
					return
				}
			}
		}()

		pythonCmd.Wait()

		delete(runningScripts, guid)
	}()

	return guid, nil
}

func getCommonCode(guid string, port string, apiKey string) string {
	newCommonCode := strings.Replace(commonCode, "PROXY_PORT", port, -1)
	newCommonCode = strings.Replace(newCommonCode, "SCRIPT_ID", guid, -1)
	newCommonCode = strings.Replace(newCommonCode, "API_KEY", apiKey, -1)

	return newCommonCode
}

func stripOutputTags(output []byte) []byte {
	return bytes.ReplaceAll(output, []byte("PROXIMITY_PYTHON_INTERPRETER_SCRIPT_FINISHED\n"), []byte(""))
}
