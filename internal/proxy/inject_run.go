package proxy

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"text/template"
	"unicode/utf8"

	"github.com/forensant/pakiki-core/internal/scripting"
	"github.com/forensant/pakiki-core/pkg/project"
)

//go:embed inject_python.py
var scriptTemplate string

func runInjection(inject *project.InjectOperation, port string, apiKey string) {
	script, err := generateScriptForInjection(inject)

	if err != nil {
		fmt.Printf("Error generating script: %s\n", err)
	}

	inject.Record()

	scriptCode := scripting.ScriptCode{
		Code:       script,
		Filename:   "script.py",
		MainScript: true,
	}

	_, err = scripting.StartScript(
		port,
		[]scripting.ScriptCode{scriptCode},
		apiKey,
		inject)

	if err != nil {
		inject.RecordError("Error running script: " + err.Error())
	}
}

type scriptTemplateParameters struct {
	Host     string
	Payloads string
	Request  string
	SSL      string
}

func generateScriptForInjection(inject *project.InjectOperation) (string, error) {
	injectionPointCount := 0
	for _, requestPart := range inject.Request {
		if requestPart.Inject {
			injectionPointCount += 1
		}
	}

	payloads := generatePayloads(inject)
	inject.TotalRequestCount = (injectionPointCount * len(payloads)) + 1 // include the initial base request
	ssl := "True"

	if !inject.SSL {
		ssl = "False"
	}

	requestJson, err := json.Marshal(inject.Request)
	if err != nil {
		return "", err
	}

	params := scriptTemplateParameters{
		Host:     project.EscapeForPython(inject.Host),
		Payloads: stringListToPython(payloads),
		Request:  project.EscapeForPython(string(requestJson)),
		SSL:      ssl,
	}
	temp, err := template.New("script").Parse(scriptTemplate)
	if err != nil {
		return "", err
	}

	var finalScript bytes.Buffer
	err = temp.Execute(&finalScript, params)
	if err != nil {
		return "", err
	}

	return finalScript.String(), nil
}

func generatePayloads(inject *project.InjectOperation) []string {
	payloads := make([]string, 0)
	for i := inject.IterateFrom; i < inject.IterateTo; i++ {
		payloads = append(payloads, strconv.Itoa(i))
	}

	for _, filename := range inject.FuzzDB {
		file, err := fuzzdb.Open(filename)
		if err != nil {
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			text := scanner.Text()
			if utf8.ValidString(text) {
				payloads = append(payloads, project.EscapeForPython(text))
			}
		}
	}

	for _, payload := range inject.CustomPayloads {
		if utf8.ValidString(payload) {
			payloads = append(payloads, payload)
		}
	}

	return payloads
}

func stringListToPython(strs []string) string {
	output := "["
	first := true
	for _, str := range strs {
		if first {
			first = false
		} else {
			output += ",\n"
		}
		output += "'" + project.EscapeForPython(str) + "'"
	}
	output += "]"
	return output
}
