package project

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/forensant/pakiki-core/internal/scripting"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var hookLibrary scripting.ScriptCode = scripting.ScriptCode{
	Code:       "",
	Filename:   "hook_library.py",
	MainScript: false,
}

var hooks []Hook

// Hook contains the details of a piece of code which can be run either before or after a request
type Hook struct {
	ID                uint `json:"-"`
	GUID              string
	Name              string
	Enabled           bool
	InternallyManaged bool
	HookType          string
	MatchRequest      bool
	MatchResponse     bool
	DisplayJson       string
	Code              string
	SortOrder         int
	ObjectType        string `gorm:"-"`
}

// HookResponse is used by the proxy to get the results after running a hook
type HookResponse struct {
	ResponseReady   chan bool
	Modified        bool
	ModifiedRequest []byte
}

func refreshHooks() {
	var hookEntries []Hook
	result := readableDatabase.Order("hooks.internally_managed").Order("hooks.hook_type").Order("hooks.sort_order").Find(&hookEntries)

	if result.Error != nil {
		fmt.Printf("Error retrieving hook entries from database: %s", result.Error)
		return
	}

	hooks = hookEntries
}

func (h *Hook) Record() {
	if h.GUID == "" {
		h.GUID = uuid.NewString()
	}

	ioHub.databaseWriter <- h

	h.ObjectType = "Hook"
	ioHub.broadcast <- h

	go refreshHooks()
}

func runHooks(req *Request, reqBytes []byte, isResponse bool) *HookResponse {
	response := &HookResponse{
		ResponseReady:   make(chan bool),
		Modified:        false,
		ModifiedRequest: reqBytes,
	}

	if req.isLarge() || hookLibrary.Code == "" || req.Protocol != "HTTP" {
		return response
	}

	direction := "request"
	if isResponse {
		direction = "response"
	}

	code := direction + " = RequestResponse({'GUID':'" + req.GUID + "'," +
		"'URL': '" + EscapeForPython(req.URL) + "'," +
		"'Verb': '" + req.Verb + "'," +
		"'ResponseStatusCode': " + strconv.Itoa(req.ResponseStatusCode) + "," +
		"'ResponseContentType': '" + EscapeForPython(req.ResponseContentType) + "'})\n"

	base64Bytes := base64.StdEncoding.EncodeToString(reqBytes)

	if isResponse {
		code += "response.parse_response(base64.b64decode('" + EscapeForPython(base64Bytes) + "'))\n"
	} else {
		code += "request.parse_request(base64.b64decode('" + EscapeForPython(base64Bytes) + "'))\n"
	}

	for _, hook := range hooks {
		if !hook.Enabled {
			continue
		}

		if isResponse {
			if hook.MatchRequest {
				continue
			}
		} else {
			if hook.MatchResponse {
				continue
			}
		}

		response.Modified = true
		code += "\n" + hook.Code + "\n"
	}

	if !response.Modified {
		return response
	}

	if isResponse {
		code += "\nprint('PAKIKI_HOOK_RESULT:' + response.response_to_base64())\n"
	} else {
		code += "\nprint('PAKIKI_HOOK_RESULT:' + request.request_to_base64())\n"
	}

	errorLog := &HookErrorLog{
		Code:         code,
		HookResponse: response,
	}

	fullCode := []scripting.ScriptCode{
		hookLibrary,
		{
			Code:       code,
			Filename:   "hooks.py",
			MainScript: true,
		},
	}

	// the error log contains the relevant hooks to do the processing after the script has run
	scripting.StartScript(ioHub.port, fullCode, ioHub.apiToken, errorLog)

	return response
}

func RunHooksOnRequest(req *Request, reqBytes []byte) *HookResponse {
	return runHooks(req, reqBytes, false)
}

func RunHooksOnResponse(req *Request, respBytes []byte) *HookResponse {
	return runHooks(req, respBytes, true)
}

func (hr *HookResponse) parseResponse(log *HookErrorLog) {
	log.Output = strings.Trim(log.Output, "\n")
	lines := strings.Split(log.Output, "\n")

	if len(lines) == 0 {
		hr.ResponseReady <- true
		return
	}

	lastLine := lines[len(lines)-1]
	hasResponse := (strings.Index(lastLine, "PAKIKI_HOOK_RESULT:") == 0)

	// Save the error log
	if len(lines) > 1 {
		if hasResponse {
			lines = lines[:len(lines)-1]
		}
		log.Output = strings.Join(lines, "\n")
		log.Record()
	}

	if !hasResponse {
		hr.ResponseReady <- true
		return
	}

	// We have a response, so let's parse it
	base64Resp := lastLine[len("PAKIKI_HOOK_RESULT:"):]
	respBytes, err := base64.StdEncoding.DecodeString(base64Resp)

	if err != nil {
		fmt.Printf("Error decoding response: %s", err)
		hr.ResponseReady <- true
		return
	}

	hr.ModifiedRequest = respBytes
	hr.ResponseReady <- true
}

func (h *Hook) ShouldFilter(str string) bool {
	return false
}

func (h *Hook) validate() error {
	if h.GUID == "" {
		h.GUID = uuid.NewString()
	}

	return nil
}

func (h *Hook) WriteToDatabase(db *gorm.DB) {
	db.Save(h)
}
