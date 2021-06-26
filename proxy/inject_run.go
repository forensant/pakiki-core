package proxy

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"dev.forensant.com/pipeline/razor/proximitycore/scripting"
)

//go:embed inject_python.py
var scriptTemplate string

func runInjection(inject *project.InjectOperation, port string, apiKey string) {
	script, err := generateScriptForInjection(inject)
	fmt.Printf("Script: %s\n", script)

	if err != nil {
		fmt.Printf("Error generating script: %s\n", err)
	}

	inject.Record()

	_, err = scripting.StartScript(port, script, inject.GUID, apiKey, inject)
	if err != nil {
		inject.RecordError("Error running script: " + err.Error())
	} else {
		fmt.Printf("Script GUID:\n%s\n", inject.GUID)
	}
}

type scriptTemplateParameters struct {
	Host      string
	Payloads  string
	PointList string
	Request   string
	SSL       string
}

type injectionPoint struct {
	offset int
	length int
}

func generateScriptForInjection(inject *project.InjectOperation) (string, error) {
	requestWithoutInjectionPoints, injectionPoints := parseInjectionPoints(inject.Request)

	payloads := generatePayloads(inject)
	inject.TotalRequestCount = (len(injectionPoints) * len(payloads)) + 1 // include the initial base request
	ssl := "True"

	if !inject.SSL {
		ssl = "False"
	}

	params := scriptTemplateParameters{
		Host:      escapeForPython(inject.Host),
		Payloads:  stringListToPython(payloads),
		PointList: injectionPointsToPython(injectionPoints),
		Request:   requestWithoutInjectionPoints,
		SSL:       ssl,
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

func escapeForPython(input string) string {
	output := strings.ReplaceAll(input, "\\", "\\\\")
	output = strings.ReplaceAll(output, "\n", "\\n")
	output = strings.ReplaceAll(output, "'", "\\'")
	output = strings.ReplaceAll(output, "\x0A", "")
	output = strings.ReplaceAll(output, "\x0D", "")
	return output
}

func generatePayloads(inject *project.InjectOperation) []string {
	payloads := make([]string, 0)
	for i := inject.IterateFrom; i < inject.IterateTo; i++ {
		payloads = append(payloads, strconv.Itoa(i))
	}

	for _, filename := range inject.FuzzDB {
		file, err := fuzzdb.Open("resources/fuzzdb/attacks/" + filename)
		if err != nil {
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			payloads = append(payloads, escapeForPython(scanner.Text()))
		}
	}

	for _, filename := range inject.KnownFiles {
		file, err := fuzzdb.Open("resources/fuzzdb/file_lists/" + filename)
		if err != nil {
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			payloads = append(payloads, escapeForPython(scanner.Text()))
		}
	}

	return payloads
}

func injectionPointsToPython(points []injectionPoint) string {
	output := "["
	first := true
	for _, point := range points {
		if first {
			first = false
		} else {
			output += ","
		}
		output += "[" + strconv.Itoa(point.offset) + ", " + strconv.Itoa(point.length) + "]"
	}

	output += "]"
	return output
}

func parseInjectionPoints(request string) (string, []injectionPoint) {
	injectionPoints := make([]injectionPoint, 0)
	for startIdx := strings.Index(request, "#{"); startIdx != -1; startIdx = strings.Index(request, "#{") {
		request = strings.Replace(request, "#{", "", 1)
		endIdx := strings.Index(request, "}")
		if endIdx == -1 {
			break
		}
		length := endIdx - startIdx
		injectionPoints = append(injectionPoints, injectionPoint{
			offset: startIdx,
			length: length,
		})
		request = strings.Replace(request, "}", "", 1)
	}

	return request, injectionPoints
}

func stringListToPython(strs []string) string {
	output := "["
	first := true
	for _, str := range strs {
		if first {
			first = false
		} else {
			output += ","
		}
		output += "'" + escapeForPython(str) + "'"
	}
	output += "]"
	return output
}
