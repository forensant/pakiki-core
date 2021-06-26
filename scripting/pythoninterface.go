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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	_ "embed"

	"github.com/google/uuid"
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

	return nil
}

func StartScript(hostPort string, script string, guid string, apiKey string, scriptCaller ScriptCaller) (string, error) {

	if guid == "" {
		guid = uuid.NewString()
	}

	path, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}

	pythonPath := path + "/pythoninterpreter"
	fmt.Println("Python path: " + pythonPath)
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

		output, err := bufferedOutput.ReadString('\n')
		fmt.Println("Output: " + output)
		if err != nil {
			err := "Error running script: " + err.Error()
			if scriptCaller != nil {
				scriptCaller.RecordError(err)
			} else {
				fmt.Println(err)
			}
			return
		}

		if output != "PROXIMITY_PYTHON_INTERPRETER_READY\n" {
			err := "Unexpected output running script: " + output
			if scriptCaller != nil {
				scriptCaller.RecordError(err)
			} else {
				fmt.Println(err)
			}
			return
		}

		pythonIn.Write([]byte(script))
		pythonIn.Write([]byte("\nPROXIMITY_PYTHON_INTERPRETER_END_INTERPRETER\n"))

		outputBytes, err := ioutil.ReadAll(pythonOut)
		if err != nil {
			fmt.Println("Error reading output from script: " + err.Error())
			return
		}

		outputToRecord := stripOutputTags(outputBytes)

		fmt.Printf("Script output: %s\n", outputToRecord)

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
