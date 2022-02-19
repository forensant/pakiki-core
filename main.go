package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

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

//go:embed docs/swagger.json
var swaggerJson string

type commandLineParameters struct {
	APIKey           string
	BindAddress      string
	ProjectPath      string
	ParentPID        int32
	APIPort          int
	PreviewProxyPort int
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
	listener := createListener(parameters.APIPort, parameters.BindAddress)
	port = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)

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

	ioHub := project.NewIOHub()
	ioHub.Run(parameters.ProjectPath)

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
		log.Printf("Warning: The proxy could not be started, %v\n", err.Error())
	}

	previewProxyListener := createListener(parameters.PreviewProxyPort, "localhost")
	err = proxy.StartHttpPreviewProxy(previewProxyListener)
	if err != nil {
		log.Printf("Warning: The preview proxy could not be started, rendering of pages will likely not work: %v\n", err.Error())
	} else {
		fmt.Printf("Preview proxy is available at: http://localhost:%d/\n", previewProxyListener.Addr().(*net.TCPAddr).Port)
	}

	proxy.StartOutOfBandClient()

	http.HandleFunc("/project/requestresponse", authenticateWithGormDB(project.GetRequestResponse))
	http.HandleFunc("/project/requests", authenticateWithGormDB(project.GetRequests))
	http.HandleFunc("/project/request", authenticateWithGormDB(project.HandleRequest))
	http.HandleFunc("/project/request/payloads", authenticateWithGormDB(project.PutRequestPayloads))
	http.HandleFunc("/project/scripts", authenticateWithGormDB(project.GetScripts))
	http.HandleFunc("/project/script", authenticateWithGormDB(project.GetScript))
	http.HandleFunc("/project/script/append_html_output", authenticateWithGormDB(project.PostAppendHTMLOutputScript))
	http.HandleFunc("/project/script/archive", authenticateWithGormDB(project.PutArchiveScript))
	http.HandleFunc("/project/script_groups", authenticateWithGormDB(project.GetScriptGroups))
	http.HandleFunc("/project/script_group", authenticateWithGormDB(project.HandleScriptGroup))
	http.HandleFunc("/project/script_group/archive", authenticateWithGormDB(project.PutArchiveScriptGroup))
	http.HandleFunc("/project/sitemap", authenticate(project.GetSitemap))

	http.HandleFunc("/proxy/add_request_to_queue", authenticate(proxy.AddRequestToQueue))
	http.HandleFunc("/proxy/ca_certificate.pem", proxy.CACertificate)
	http.HandleFunc("/proxy/intercepted_requests", authenticate(proxy.GetInterceptedRequests))
	http.HandleFunc("/proxy/intercept_settings", authenticate(proxy.HandleInterceptSettingsRequest))
	http.HandleFunc("/proxy/make_request", authenticate(proxy.MakeRequest))
	http.HandleFunc("/proxy/out_of_band/url", authenticate(proxy.GetOOBURL))
	http.HandleFunc("/proxy/ping", ping)
	http.HandleFunc("/proxy/set_intercepted_response", authenticate(proxy.SetInterceptedResponse))
	http.HandleFunc("/proxy/settings", authenticate(proxy.HandleSettingsRequest))

	http.HandleFunc("/inject_operations/fuzzdb_payload", authenticate(proxy.GetFuzzdbPayload))
	http.HandleFunc("/inject_operations/payloads", authenticate(proxy.GetInjectPayloads))
	http.HandleFunc("/inject_operations/run", authenticate(proxy.RunInjection))
	http.HandleFunc("/inject_operations", authenticateWithGormDB(project.GetInjectOperations))
	http.HandleFunc("/inject_operation", authenticateWithGormDB(project.HandleInjectOperation))
	http.HandleFunc("/inject_operation/archive", authenticateWithGormDB(project.PutArchiveInjectOperation))

	http.HandleFunc("/scripts/cancel", authenticate(scripting.CancelScript))
	http.HandleFunc("/scripts/run", authenticate(scripting.RunScript))
	http.HandleFunc("/scripts/update_progress", authenticate(scripting.UpdateProgress))

	http.HandleFunc("/project/notifications", authenticate(func(w http.ResponseWriter, r *http.Request) {
		project.Notifications(ioHub, apiToken, w, r)
	}))
	http.HandleFunc("/debug", authenticate(project.Debug))

	http.HandleFunc("/api_key.js", handleAPIKey)
	http.HandleFunc("/swagger/", httpSwagger.Handler(httpSwagger.URL("http://localhost:"+port+"/swagger/doc.json")))
	http.HandleFunc("/swagger/doc.json", handleSwaggerJSON)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	srv := &http.Server{
		Handler: logRequest(http.DefaultServeMux),
	}

	go func() {
		err := srv.Serve(listener)

		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("%s", err)
		}
	}()

	<-done

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		proxy.CloseOutOfBandClient()
		cancel()
	}()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server Shutdown Failed:%+v", err)
	}
}

func addCorsHeaders(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", req.Header.Get("Origin")) // all requests have API keys, so we're not worried about CORS attacks
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-API-Key")
}

func authenticateAndProcessRequest(w http.ResponseWriter, r *http.Request) bool {
	addCorsHeaders(&w, r)

	// allow the pre-flight requests as they don't have API Keys, but don't actually execute them
	if r.Method == http.MethodOptions {
		w.Write([]byte("OK"))
		return false
	}

	headerAPIKey := r.Header.Get("X-API-Key")
	formAPIKey := r.FormValue("api_key")

	if headerAPIKey != apiToken && formAPIKey != apiToken {
		fmt.Println("Invalid API key")
		w.Header().Add("WWW-Authenticate", "API Key")
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
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

func createListener(portParameter int, hostname string) net.Listener {
	listener, err := net.Listen("tcp", hostname+":"+strconv.Itoa(portParameter))
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			fmt.Printf("Error: Port %d is already in use, could not use it for the UI. Using a random one.\n", portParameter)
			return createListener(0, hostname)
		} else {
			panic(err)
		}
	}

	return listener
}

func ensureProcessExists(parentPID int32) {
	exists, err := process.PidExists(parentPID)
	if err != nil {
		log.Println("Could not check whether parent process exists: ", err)
		return
	}

	if !exists {
		log.Fatal("Parent process ended, killing proxy")
	}
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	n, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	if n != 32 {
		return "", errors.New("did not generate 32 bytes")
	}

	dst := make([]byte, hex.EncodedLen(len(b)))
	hex.Encode(dst, b)

	key := string(dst)

	fmt.Printf("API Key: %s\n", key)

	return key, nil
}

func handleAPIKey(w http.ResponseWriter, r *http.Request) {
	key := ""
	if isLocalhost(r) {
		key = apiToken
	}

	js := "CORE_API_KEY = '" + key + "'"
	w.Write([]byte(js))
}

func handleSwaggerJSON(w http.ResponseWriter, r *http.Request) {
	data := strings.ReplaceAll(swaggerJson, "\"host\": \"localhost\",", "\"host\": \"localhost:"+port+"\",")

	w.Write([]byte(data))
}

func isLocalhost(r *http.Request) bool {
	hostnameComponents := strings.Split(r.Host, ":")
	return (hostnameComponents[0] == "localhost" || hostnameComponents[0] == "127.0.0.1")
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("API Request Received: %s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
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
	bindAddressPtr := flag.String("bind-address", "localhost", "The address to bind the API and UI to")
	parentPIDInt := flag.Int("parentpid", 0, "The process id (PID) of the proxy parent process")
	projectPathPtr := flag.String("project", "", "The path to the project to open")
	apiPortPtr := flag.Int("api-port", 10101, "The port for the API and UI to listen on (set to 0 for a random available port)")
	previewProxyPortPtr := flag.Int("preview-proxy-port", 10111, "The port for the preview proxy to listen on (set to 0 for a random available port)")

	flag.Parse()

	parentPID := int32(*parentPIDInt)

	if *projectPathPtr == "" {
		log.Fatal("A project path must be specified")
	}

	params := commandLineParameters{
		*apiKeyPtr,
		*bindAddressPtr,
		*projectPathPtr,
		parentPID,
		*apiPortPtr,
		*previewProxyPortPtr,
	}

	if params.APIKey == "" {
		var err error
		params.APIKey, err = generateAPIKey()
		if err != nil {
			log.Fatal("Unable to generate API key: " + err.Error())
		}
	}

	return params
}

func ping(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}
