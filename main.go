package main

import (
	"bytes"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "embed"

	httpSwagger "github.com/swaggo/http-swagger" // http-swagger middleware
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	process "github.com/shirou/gopsutil/process"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"dev.forensant.com/pipeline/razor/proximitycore/proxy"
	"dev.forensant.com/pipeline/razor/proximitycore/scripting"
)

var apiToken, port string
var db *sql.DB
var gormDB *gorm.DB

//go:embed html_frontend/dist/*
var frontendDir embed.FS

type commandLineParameters struct {
	APIKey      string
	ProjectPath string
	ParentPID   int32
	UIPort      int
}

// @title Proximity Core
// @version 1.0
// @description This provides the common functions which are relied upon by the Proximity frontends.
// @termsOfService https://forensant.com/terms/

// @contact.name API Support
// @contact.url https://forensant.com/support
// @contact.email support@forensant.com

// @license.name Commercial
// @license.url https://proximity.forensant.com/terms

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key

// @host localhost
// @BasePath
func main() {
	parameters := parseCommandLineFlags()
	listener := createListener(parameters.UIPort)

	if parameters.ParentPID != 0 {
		monitorParentProcess(parameters.ParentPID)
	}

	apiToken = parameters.APIKey
	var err error
	databasePath := parameters.ProjectPath + "?mode=ro&_busy_timeout=5000"
	db, err = sql.Open("sqlite3", databasePath)
	if err != nil {
		log.Fatal("Could not open the database: " + err.Error())
		return
	}
	defer db.Close()

	gormDB, err = gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	fmt.Printf("Web frontend is available at: http://localhost:%s/\n", port)
	frontendSubdirectory, err := fs.Sub(frontendDir, "html_frontend/dist")
	if err != nil {
		log.Fatal("Could not open the subdirectory for the frontend: " + err.Error())
		return
	}
	frontendFilesystem := http.FileServer(http.FS(frontendSubdirectory))
	http.Handle("/", frontendFilesystem)

	err = proxy.StartListeners()
	if err != nil {
		log.Printf("Warning: Could not start the proxy listeners: %v\n", err.Error())
	}

	http.HandleFunc("/project/requestresponse", authenticateWithGormDB(project.GetRequestResponse))
	http.HandleFunc("/project/requests", authenticateWithGormDB(project.GetRequests))
	http.HandleFunc("/project/request", authenticateWithGormDB(project.HandleRequest))
	http.HandleFunc("/project/scripts", authenticateWithGormDB(project.GetScripts))
	http.HandleFunc("/project/script", authenticateWithGormDB(project.GetScript))
	http.HandleFunc("/project/script/append_html_output", authenticateWithGormDB(project.PostAppendHTMLOutputScript))
	http.HandleFunc("/project/script/archive", authenticateWithGormDB(project.PutArchiveScript))
	http.HandleFunc("/project/sitemap", authenticate(project.GetSitemap))

	http.HandleFunc("/proxy/add_request_to_queue", authenticate(proxy.AddRequestToQueue))
	http.HandleFunc("/proxy/ca_certificate.pem", proxy.CACertificate)
	http.HandleFunc("/proxy/intercepted_requests", authenticate(proxy.GetInterceptedRequests))
	http.HandleFunc("/proxy/intercept_settings", authenticate(proxy.HandleInterceptSettingsRequest))
	http.HandleFunc("/proxy/make_request", authenticate(proxy.MakeRequest))
	http.HandleFunc("/proxy/set_intercepted_response", authenticate(proxy.SetInterceptedResponse))
	http.HandleFunc("/proxy/settings", authenticate(proxy.HandleSettingsRequest))

	http.HandleFunc("/inject_operations/payloads", authenticate(proxy.GetInjectPayloads))
	http.HandleFunc("/inject_operations/run", authenticate(proxy.RunInjection))
	http.HandleFunc("/inject_operations", authenticateWithGormDB(project.GetInjectOperations))
	http.HandleFunc("/inject_operation", authenticateWithGormDB(project.PutInjectOperation))
	http.HandleFunc("/inject_operation/archive", authenticateWithGormDB(project.PutArchiveInjectOperation))

	http.HandleFunc("/scripts/cancel", authenticate(scripting.CancelScript))
	http.HandleFunc("/scripts/run", authenticate(scripting.RunScript))
	http.HandleFunc("/scripts/update_progress", authenticate(scripting.UpdateProgress))

	ioHub := project.NewIOHub()
	go ioHub.Run(parameters.ProjectPath)
	http.HandleFunc("/project/notifications", authenticate(func(w http.ResponseWriter, r *http.Request) {
		project.Notifications(ioHub, apiToken, w, r)
	}))
	http.HandleFunc("/debug", authenticate(project.Debug))

	http.HandleFunc("/swagger/", httpSwagger.Handler(httpSwagger.URL("http://localhost:"+port+"/swagger/doc.json")))
	http.HandleFunc("/swagger/doc.json", handleSwaggerJSON)

	log.Fatal(http.Serve(listener, nil))
}

func addCorsHeaders(w *http.ResponseWriter, req *http.Request) {
	if project.IsValidOrigin(req, apiToken) {
		(*w).Header().Set("Access-Control-Allow-Origin", req.Header.Get("Origin"))
		(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-API-Key")
	}
}

func authenticateAndProcessRequest(w http.ResponseWriter, r *http.Request) bool {
	addCorsHeaders(&w, r)
	clientAPIKey := r.Header.Get("X-API-Key")
	authenticated := false
	if apiToken != "" && clientAPIKey == apiToken {
		authenticated = true
	} else if apiToken != "" {
		fmt.Println("Invalid API key")
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return false
	}

	if !authenticated && !isLocalhost(r.RemoteAddr) {
		fmt.Println("Unauthorised")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	if r.Method == http.MethodOptions {
		return false
	}

	return true
}

func authenticate(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authenticateAndProcessRequest(w, r) {
			fn(w, r)
		}
	}
}

func authenticateWithGormDB(fn func(http.ResponseWriter, *http.Request, *gorm.DB)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authenticateAndProcessRequest(w, r) {
			fn(w, r, gormDB)
		}
	}
}

func createListener(portParameter int) net.Listener {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(portParameter))
	if err != nil {
		panic(err)
	}

	port = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	fmt.Println("Listening on port:", port)

	return listener
}

func ensureProcessExists(parentPID int32) {
	exists, err := process.PidExists(parentPID)
	if err != nil {
		log.Println("Could not check whether parent process exists: ", err)
		return
	}

	if exists == false {
		log.Fatal("Parent process ended, killing proxy")
	}
}

func handleSwaggerJSON(w http.ResponseWriter, r *http.Request) {
	dat, err := ioutil.ReadFile("docs/swagger.json")
	if err != nil {
		http.Error(w, "Could not open swagger json: "+err.Error(), http.StatusInternalServerError)
	}

	dat = bytes.ReplaceAll(dat, []byte("\"host\": \"localhost\","), []byte("\"host\": \"localhost:"+port+"\","))

	w.Write(dat)
}

func isLocalhost(remoteAddr string) bool {
	portIdx := strings.LastIndex(remoteAddr, ":")

	if portIdx == -1 {
		return false
	}

	remoteAddr = remoteAddr[0:portIdx]

	return remoteAddr == "[::1]" || remoteAddr == "127.0.0.1"
}

func monitorParentProcess(parentPID int32) {
	ticker := time.NewTicker(time.Second)

	go func() {
		for {
			select {
			case <-ticker.C:
				ensureProcessExists(parentPID)
			}
		}
	}()
}

func parseCommandLineFlags() commandLineParameters {
	apiKeyPtr := flag.String("api-key", "", "A key required to be passed to the X-API-Key header for every request")
	parentPIDInt := flag.Int("parentpid", 0, "The process id (PID) of the proxy parent process")
	projectPathPtr := flag.String("project", "", "The path to the project to open")
	uiPortPtr := flag.Int("port", 10101, "The port for the API and UI to listen on (set to 0 for a random available port)")

	flag.Parse()

	parentPID := int32(*parentPIDInt)

	if *projectPathPtr == "" {
		log.Fatal("A project path must be specified")
	}

	return commandLineParameters{
		*apiKeyPtr,
		*projectPathPtr,
		parentPID,
		*uiPortPtr,
	}
}
