package scripting

// Design decisions: We're spawning a separate Python process to get around the
// Python Global Interpreter Lock (GIL). You can do multi-threaded code by embeding it in Go, but
// with the GIL there is a possibility of bugs if you're not concious of what variables you're updating and when.
// It was decided that it'd be easier to have each Python script run independently of each other in their own process.

// Python was chosen because a lot of people in the infosec community already are familiar with it, and it has a great
// standard library. It does have the tradeoff that it can be a pain to get the environment nicely working when
// deploying the application.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	_ "embed"
)

//go:embed pakiki_core.py
var commonCode string

var runningScripts = make(map[string]*exec.Cmd)

// ScriptCode contains an individual file to be run as part of a script
type ScriptCode struct {
	Code       string
	Filename   string
	MainScript bool
}

type ScriptCaller interface {
	GetGUID() string
	RecordError(string)

	SetFullOutput(string)
	SetOutput(string)
	SetStatus(string)
}

func CancelScriptInternal(guid string) error {
	command, ok := runningScripts[guid]

	if ok {
		err := command.Process.Kill()
		if err != nil {
			return err
		}
	}

	return nil
}

func readFromBuffer(r io.ReadCloser, waitForNewline bool) (string, error) {
	readBuf := make([]byte, 4096)
	output := make([]byte, 0)

	for {
		bytesRead, err := r.Read(readBuf)
		if bytesRead != 0 {
			output = append(output, readBuf[:bytesRead]...)
			if bytes.Contains(output, []byte("\n")) || !waitForNewline {
				return string(output), nil
			}
		} else {
			return "", err
		}
	}
}

func replaceCodeVariables(code string, guid string, port string, apiKey string) string {
	code = strings.ReplaceAll(code, "PAKIKI_PROXY_PORT", port)
	code = strings.ReplaceAll(code, "PAKIKI_SCRIPT_ID", guid)
	code = strings.ReplaceAll(code, "PAKIKI_API_KEY", apiKey)

	return code
}

func startPythonInterpreter(guid string) (stdin io.WriteCloser, stdout io.ReadCloser, stderr io.ReadCloser, err error) {
	executableName := "/pakikipythoninterpreter"
	if runtime.GOOS == "windows" {
		executableName = "\\pakikipythoninterpreter.exe"
	}

	executablePath, err := os.Executable()
	if err != nil {
		return
	}
	executablePath = filepath.Dir(executablePath)
	pythonPath := executablePath + executableName

	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		executablePath, err = os.Getwd()
		if err != nil {
			log.Println(err)
		}
		pythonPath = executablePath + executableName
	}

	if _, err = os.Stat(pythonPath); os.IsNotExist(err) {
		err = errors.New("could not find Python interpreter")
		return
	}

	pythonCmd := exec.Command(pythonPath)
	stdin, err = pythonCmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err = pythonCmd.StdoutPipe()
	if err != nil {
		return
	}

	stderr, err = pythonCmd.StderrPipe()
	if err != nil {
		return
	}

	err = pythonCmd.Start()
	if err != nil {
		return
	}

	runningScripts[guid] = pythonCmd

	return
}

func StartScript(hostPort string, scriptCode []ScriptCode, apiKey string, scriptCaller ScriptCaller) (string, error) {
	guid := scriptCaller.GetGUID()

	pythonIn, pythonOut, pythonErr, err := startPythonInterpreter(guid)
	if err != nil {
		return "", err
	}

	commonScriptCode := ScriptCode{
		Code:     commonCode,
		Filename: "pakiki_core.py",
	}

	scriptCode = append([]ScriptCode{commonScriptCode}, scriptCode...)

	scriptCaller.SetStatus("Running")

	go func() {
		for idx, scriptPart := range scriptCode {
			code := replaceCodeVariables(scriptPart.Code, guid, hostPort, apiKey)
			err = sendCodeToInterpreter(scriptPart.Filename, code, pythonIn, pythonErr, idx == len(scriptCode)-1)

			if err != nil {
				err := "Error running script: " + err.Error()
				fmt.Println(err + "\n")

				if scriptCaller != nil {
					scriptCaller.RecordError(err)
				} else {
					fmt.Println(err)
				}

				scriptCaller.RecordError(err)

				runningScripts[guid].Process.Kill()
				delete(runningScripts, guid)
				return
			}
		}

		pythonIn.Write([]byte("\nPAKIKI_PYTHON_INTERPRETER_END_OF_SCRIPT\n"))

		readingFinishedChannel := make(chan bool)
		go func() {
			readBuf := make([]byte, 1024)
			fullOutput := make([]byte, 0)
			for {
				bytesRead, err := pythonOut.Read(readBuf)
				lineRead := readBuf[:bytesRead]

				errStr := ""
				if bytes.Contains(lineRead, []byte("PAKIKI_PYTHON_INTERPRETER_ERROR")) {
					errStr = string(stripOutputTags(lineRead))
				} else if bytesRead != 0 {
					fullOutput = stripOutputTags(append(fullOutput, lineRead...))
					scriptCaller.SetOutput(string(stripOutputTags(lineRead)))
				}

				if err != nil || errStr != "" { // will indicate that the file has been closed
					scriptCaller.SetFullOutput(string(fullOutput))
					if errStr != "" {
						scriptCaller.RecordError(errStr)
					} else {
						scriptCaller.SetStatus("Completed")
					}

					readingFinishedChannel <- true
					return
				}
			}
		}()

		runningScripts[guid].Wait()
		<-readingFinishedChannel

		delete(runningScripts, guid)
	}()

	return guid, nil
}

func stripOutputTags(output []byte) []byte {
	output = bytes.ReplaceAll(output, []byte("PAKIKI_PYTHON_INTERPRETER_SCRIPT_FINISHED\n"), []byte(""))
	output = bytes.ReplaceAll(output, []byte("PAKIKI_PYTHON_INTERPRETER_ERROR\n"), []byte(""))
	output = bytes.ReplaceAll(output, []byte("PAKIKI_PYTHON_INTERPRETER_READY\n"), []byte(""))
	return output
}

func sendCodeToInterpreter(filename string, code string, stdin io.WriteCloser, stderr io.ReadCloser, lastBlock bool) error {
	_, err := stdin.Write([]byte(filename + "\n"))
	if err != nil {
		fmt.Println("Error writing bytes to process: " + err.Error())
		return err
	}

	stdin.Write([]byte(code))
	if lastBlock {
		return nil
	}
	stdin.Write([]byte("\n\nPAKIKI_PYTHON_INTERPRETER_END_OF_BLOCK\n"))

	output, err := readFromBuffer(stderr, true)
	if err != nil {
		return err
	}

	output = strings.TrimSpace(output)
	if !strings.Contains(output, "PAKIKI_PYTHON_INTERPRETER_READY") && !strings.Contains(output, "PAKIKI_PYTHON_INTERPRETER_SCRIPT_FINISHED") {
		allOutput := make([]byte, 10240)
		stderr.Read(allOutput)
		fullOutput := append([]byte(output), allOutput...)
		fullOutput = stripOutputTags(fullOutput)
		outputStr := string(fullOutput)
		outputStr = strings.TrimSpace(outputStr)

		return errors.New("unexpected output running script: " + outputStr)
	}

	return nil
}
