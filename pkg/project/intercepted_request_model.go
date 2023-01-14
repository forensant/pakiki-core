package project

// this is reasonably minimal, as we don't actually want to save these to the database

// InterceptedRequest contains the parameters which hold the details of intercepted requests
type InterceptedRequest struct {
	Request       *Request
	GUID          string
	Body          string    `example:"<base64 encoded body>"`
	Direction     string    `example:"Either browser_to_server or server_to_browser"`
	ResponseReady chan bool `json:"-"`
	ObjectType    string
	RecordAction  string `example:"Either add or delete"`
	RequestAction string `json:"-" example:"One of forward, forward_and_intercept_response or drop"`
	IsUTF8        bool
}

const (
	RecordActionAdd    = 1
	RecordActionDelete = 2
)

func (interceptedRequest *InterceptedRequest) Record(recordAction int) {
	if recordAction == RecordActionAdd {
		interceptedRequest.RecordAction = "add"
	} else {
		interceptedRequest.RecordAction = "delete"
	}

	interceptedRequest.ObjectType = "Intercepted Request"

	ioHub.broadcast <- interceptedRequest
}

func (interceptedRequest *InterceptedRequest) ShouldFilter(str string) bool {
	return false
}
