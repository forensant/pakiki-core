package main

import (
	"context"
	"crypto/rand"
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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	httpSwagger "github.com/swaggo/http-swagger" // http-swagger middleware
	"gorm.io/gorm"

	"github.com/gorilla/mux"
	process "github.com/shirou/gopsutil/process"

	assets "github.com/forensant/pakiki-core"
	"github.com/forensant/pakiki-core/internal/proxy"
	"github.com/forensant/pakiki-core/pkg/project"
)

var apiToken, port string
var gormDB *gorm.DB
var shouldCleanupProjectSettings bool

var crashHandlingEnabled bool = false
var release string = "development"

type commandLineParameters struct {
	APIKey           string
	BindAddress      string
	ProjectPath      string
	TempProjectPath  string
	ParentPID        int32
	APIPort          int
	PreviewProxyPort int
}

// @title Pākiki Core
// @version 1.0
// @description This provides the common functions which are relied upon by the Pākiki frontends.
// @termsOfService https://pakikiproxy.com/legal/

// @contact.name API Support
// @contact.url https://pakikiproxy.com/
// @contact.email support@pakikiproxy.com

// @license.name MIT

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key

// @host localhost
// @BasePath
func main() {
	parameters := parseCommandLineFlags()

	err := sentry.Init(sentry.ClientOptions{
		Dsn:                "https://aaf0b2237ce651346d15ff46debe9ff3@sentryio.pakikiproxy.com/3",
		AttachStacktrace:   true,
		Release:            release,
		EnableTracing:      true,
		TracesSampleRate:   0.1,
		ProfilesSampleRate: 0.1,
		ServerName:         "-",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if !crashHandlingEnabled {
				return nil
			}
			return event
		},
	})

	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(5 * time.Second)
	defer sentry.Recover()

	listener := createListener(parameters.APIPort, parameters.BindAddress)
	port = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)

	if parameters.ParentPID != 0 {
		monitorParentProcess(parameters.ParentPID)
	}

	apiToken = parameters.APIKey

	projectPath, tempDBPath, shouldSave := getProjectPath(parameters.ProjectPath, parameters.TempProjectPath)
	shouldCleanupProjectSettings = shouldSave

	ioHub := project.NewIOHub(port, apiToken)
	gormDB, tempDBPath = ioHub.Run(projectPath, tempDBPath)

	if gormDB == nil {
		panic("failed to connect database")
	}

	if shouldSave {
		err := saveDatabasePaths(projectPath, tempDBPath)
		if err != nil {
			panic("could not save database settings: " + err.Error())
		}
	}

	fmt.Printf("Web frontend is available at: http://%s/\n", listener.Addr().String())
	frontendSubdirectory, err := fs.Sub(assets.HTMLFrontendDir, "www/html_frontend/dist")
	if err != nil {
		log.Fatal("Could not open the subdirectory for the frontend: " + err.Error())
		return
	}
	frontendFilesystem := http.FileServer(http.FS(frontendSubdirectory))

	browserHomeSubdir, err := fs.Sub(assets.BrowserHomepageDir, "www/browser_home")
	if err != nil {
		log.Fatal("Could not open the subdirectory for the browser homepage: " + err.Error())
		return
	}
	browserHomeFilesystem := http.FileServer(http.FS(browserHomeSubdir))

	docsSubdir, err := fs.Sub(assets.DocsDir, "docs/pakiki-documentation")
	if err != nil {
		log.Fatal("Could not open the subdirectory for the documentation: " + err.Error())
		return
	}
	docsFilesystem := http.FileServer(http.FS(docsSubdir))

	cyberchefDir, err := fs.Sub(assets.CyberChefDir, "www/cyberchef/build/prod")
	if err != nil {
		log.Fatal("Could not open the subdirectory for CyberChef: " + err.Error())
		return
	}
	cyberchefFilesystem := http.FileServer(http.FS(cyberchefDir))

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

	go proxy.StartOutOfBandClient()

	sentryHandler := sentryhttp.New(sentryhttp.Options{})

	rtr := mux.NewRouter()

	rtr.Use(sentryHandler.Handle)
	rtr.HandleFunc("/crash_reporting/test", authenticate(crash))
	rtr.HandleFunc("/crash_reporting", authenticate(handleCrashReporting))
	rtr.HandleFunc("/hooks", authenticate(project.HandleHooks))
	rtr.HandleFunc("/hooks/errors", authenticate(project.GetHookErrors))
	rtr.HandleFunc("/hooks/errors/{guid}", authenticate(project.DeleteHookError))
	rtr.HandleFunc("/hooks/order", authenticate(project.OrderHooks))
	rtr.HandleFunc("/hooks/set_library", authenticate(project.SetHookLibrary))
	rtr.HandleFunc("/hooks/{guid}/enabled", authenticate(project.EnableHook))
	rtr.HandleFunc("/hooks/{guid}", authenticate(project.DeleteHook))

	rtr.HandleFunc("/inject_operations", authenticateWithGormDB(project.GetInjectOperations))
	rtr.HandleFunc("/inject_operations/fuzzdb_payload", authenticate(proxy.GetFuzzdbPayload))
	rtr.HandleFunc("/inject_operations/payloads", authenticate(proxy.GetInjectPayloads))
	rtr.HandleFunc("/inject_operations/run", authenticate(proxy.RunInjection))
	rtr.HandleFunc("/inject_operations/{guid}", authenticate(project.GetInjectOperation))
	rtr.HandleFunc("/inject_operations/{guid}/archive", authenticateWithGormDB(project.PatchInjectOperationArchive))
	rtr.HandleFunc("/inject_operations/{guid}/title", authenticateWithGormDB(project.PatchInjectOperationTitle))

	rtr.HandleFunc("/out_of_band/url", authenticate(proxy.GetOOBURL))

	rtr.HandleFunc("/proxy/ca_certificate.pem", proxy.CACertificate)
	rtr.HandleFunc("/proxy/intercepted_requests", authenticate(proxy.GetInterceptedRequests))
	rtr.HandleFunc("/proxy/intercept_settings", authenticate(proxy.HandleInterceptSettingsRequest))
	rtr.HandleFunc("/proxy/set_intercepted_response", authenticate(proxy.SetInterceptedResponse))
	rtr.HandleFunc("/proxy/settings", authenticate(proxy.HandleSettingsRequest))

	rtr.HandleFunc("/requests", authenticate(project.GetRequests))
	rtr.HandleFunc("/requests/bulk_queue", authenticate(proxy.BulkRequestQueue))
	rtr.HandleFunc("/requests/highlight", authenticate(project.HighlightRequest))
	rtr.HandleFunc("/requests/make", authenticate(proxy.MakeRequest))
	rtr.HandleFunc("/requests/queue", authenticate(proxy.AddRequestToQueue))
	rtr.HandleFunc("/requests/sitemap", authenticate(project.GetSitemap))
	rtr.HandleFunc("/requests/{base_guid}/compare/{compare_guid}", authenticateWithGormDB(project.CompareRequests))
	rtr.HandleFunc("/requests/{base_guid}/compare_by_block/{compare_guid}", authenticateWithGormDB(project.CompareRequestsByBlock))
	rtr.HandleFunc("/requests/{guid}", authenticateWithGormDB(project.GetRequest))
	rtr.HandleFunc("/requests/{guid}/contents", authenticate(project.GetRequestResponseContents))
	rtr.HandleFunc("/requests/{guid}/notes", authenticate(project.PatchRequestNotes))
	rtr.HandleFunc("/requests/{guid}/partial_data", authenticateWithGormDB(project.GetRequestPartialData))
	rtr.HandleFunc("/requests/{guid}/payloads", authenticateWithGormDB(project.PatchRequestPayloads))
	rtr.HandleFunc("/requests/{guid}/search", authenticateWithGormDB(project.RequestDataSearch))

	rtr.HandleFunc("/scope/entries", authenticate(project.GetScopeEntries))
	rtr.HandleFunc("/scope/entry/{guid}", authenticate(project.DeleteScopeEntry))
	rtr.HandleFunc("/scope/entry", authenticate(project.PostScopeEntry))
	rtr.HandleFunc("/scope/import", authenticate(project.ImportScope))
	rtr.HandleFunc("/scope/order", authenticate(project.OrderScopeEntries))
	rtr.HandleFunc("/scope/url_in_scope", authenticate(project.URLInScope))

	rtr.HandleFunc("/scans/{scanid}/status_statistics", authenticate(project.GetScanStatusStats))
	rtr.HandleFunc("/scans/{scanid}/unique_responses", authenticate(project.GetScanUniqueResponses))

	rtr.HandleFunc("/script_groups", authenticateWithGormDB(project.HandleScriptGroups))
	rtr.HandleFunc("/script_groups/{guid}", authenticateWithGormDB(project.GetScriptGroup))
	rtr.HandleFunc("/script_groups/{guid}/archive", authenticateWithGormDB(project.PatchScriptGroupArchive))
	rtr.HandleFunc("/script_groups/{guid}/expanded", authenticateWithGormDB(project.PatchScriptGroupExpanded))
	rtr.HandleFunc("/script_groups/{guid}/export", authenticateWithGormDB(project.ExportScriptGroup))
	rtr.HandleFunc("/script_groups/{guid}/title", authenticateWithGormDB(project.PatchScriptGroupTitle))

	rtr.HandleFunc("/scripts", authenticateWithGormDB(project.GetScripts))
	rtr.HandleFunc("/scripts/run", authenticate(project.RunScript))
	rtr.HandleFunc("/scripts/{guid}", authenticateWithGormDB(project.GetScript))
	rtr.HandleFunc("/scripts/{guid}/append_html_output", authenticateWithGormDB(project.PostAppendHTMLOutputScript))
	rtr.HandleFunc("/scripts/{guid}/archive", authenticateWithGormDB(project.PatchArchiveScript))
	rtr.HandleFunc("/scripts/{guid}/cancel", authenticate(project.CancelScriptAPI))
	rtr.HandleFunc("/scripts/{guid}/export", authenticateWithGormDB(project.ExportScriptResults))
	rtr.HandleFunc("/scripts/{guid}/update_progress", authenticate(project.UpdateProgress))

	rtr.HandleFunc("/ping", ping)

	rtr.HandleFunc("/notifications", authenticate(func(w http.ResponseWriter, r *http.Request) {
		project.Notifications(ioHub, apiToken, w, r)
	}))
	rtr.HandleFunc("/debug", project.Debug)

	rtr.HandleFunc("/api_key.js", handleAPIKey)
	rtr.HandleFunc("/swagger/doc.json", handleSwaggerJSON)

	rtr.PathPrefix("/swagger/").Handler(httpSwagger.Handler(
		httpSwagger.URL("http://localhost:"+port+"/swagger/doc.json"), //The url pointing to API definition
		httpSwagger.DeepLinking(true),
		httpSwagger.DocExpansion("none"),
		httpSwagger.DomID("#swagger-ui"),
	))

	rtr.PathPrefix("/browser_home/").Handler(http.StripPrefix("/browser_home/", browserHomeFilesystem))
	rtr.PathPrefix("/docs/").Handler(http.StripPrefix("/docs", docsFilesystem))
	rtr.PathPrefix("/cyberchef/").Handler(http.StripPrefix("/cyberchef/", cyberchefFilesystem))
	rtr.PathPrefix("/_media/").Handler(http.StripPrefix("", docsFilesystem))
	rtr.PathPrefix("/").Handler(http.StripPrefix("/", frontendFilesystem))

	http.Handle("/", rtr)

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	shutdownErr := srv.Shutdown(ctx)

	defer func() {
		cleanup()
		cancel()
	}()

	if shutdownErr != nil {
		log.Fatalf("Server Shutdown Failed:%+v", shutdownErr)
	}
}

func addCorsHeaders(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", req.Header.Get("Origin")) // all requests have API keys, so we're not worried about CORS attacks
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, PATCH")
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

func cleanup() {
	err := proxy.StopListeners()
	if err != nil {
		fmt.Printf("Could not stop proxy listeners: %s", err)
	}

	proxy.CloseOutOfBandClient()
	project.CloseProject()

	if shouldCleanupProjectSettings {
		settings, err := proxy.GetSettings()
		if err != nil {
			log.Printf("Error getting settings: %v\n", err.Error())
			return
		}
		settings.OpenFile = ""
		settings.OpenTempFile = ""
		settings.OpenProcessPID = 0
		err = proxy.SaveSettings(settings)
		if err != nil {
			log.Printf("Error saving settings: %v\n", err.Error())
		}
	}
}

func crash(w http.ResponseWriter, r *http.Request) {
	go func() {
		sentry.CurrentHub().Clone()
		defer sentry.Flush(5 * time.Second)
		defer sentry.Recover()

		panic("Test crash")
	}()
}

func createListener(portParameter int, hostname string) net.Listener {
	listener, err := net.Listen("tcp4", hostname+":"+strconv.Itoa(portParameter))
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "Only one usage of each socket address") {
			fmt.Printf("Error: Port %d is already in use, could not use it for the UI. Using a random one.\n", portParameter)
			return createListener(0, hostname)
		} else {
			fmt.Fprintf(os.Stderr, "Error starting listener: %s", err)
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
		cleanup()
		log.Fatal("Parent process ended, proxy killed")
	}
}

func fileExists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
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

func getProjectPath(requested string, requestedTempPath string) (projectPath string, tempFilePath string, shouldSave bool) {
	settings, err := proxy.GetSettings()
	projectPath = requested
	shouldSave = false
	if err != nil {
		log.Printf("Error getting settings: %v\n", err.Error())
		return
	}

	projectPath, err = filepath.Abs(requested) // default case
	if err != nil {
		log.Printf("Error getting absolute path: %v\n", err.Error())
		return
	}

	if requestedTempPath != "" {
		// effectively the GUI will be managing the temp files
		tempFilePath = requestedTempPath
		shouldSave = false
		return
	}
	shouldSave = true

	if settings.OpenTempFile == "" || settings.OpenFile == "" {
		return
	}

	tempFileExists, err := fileExists(settings.OpenTempFile)
	if !tempFileExists {
		log.Printf("The temp file %s does not exist.\n", settings.OpenTempFile)
		return
	}
	if err != nil {
		log.Printf("Error checking whether temp project file exists: %v\n", err.Error())
		return
	}

	openFileExists, err := fileExists(settings.OpenFile)
	if !openFileExists {
		log.Printf("The project file %s does not exist.\n", settings.OpenFile)
		return
	}
	if err != nil {
		log.Printf("Error checking whether open project file exists: %v\n", err.Error())
		return
	}

	pidExists, err := process.PidExists(settings.OpenProcessPID)
	if err != nil {
		log.Printf("Error checking whether open project process exists: %v\n", err.Error())
		return
	}

	if pidExists && settings.OpenProcessPID != 0 {
		log.Printf("The process was found, not saving...\n")
		shouldSave = false
		return
	}

	var input string
	for input != "y" && input != "n" {
		fmt.Printf("A previous project was not closed properly (%s). Do you want to restore it? (y/n)\n", settings.OpenFile)
		fmt.Scanf("%s", &input)
		input = strings.ToLower(input)
	}

	if input == "y" {
		projectPath = settings.OpenFile
		tempFilePath = settings.OpenTempFile
	} else if input == "n" {
		os.Remove(settings.OpenTempFile)
	}

	return
}

func handleAPIKey(w http.ResponseWriter, r *http.Request) {
	key := ""
	if isLocalhost(r) {
		key = apiToken
	}

	js := "CORE_API_KEY = '" + key + "'"
	w.Write([]byte(js))
}

func handleCrashReporting(w http.ResponseWriter, r *http.Request) {
	crashHandlingEnabled = r.FormValue("enabled") == "true"

	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{
			Email: r.FormValue("user"),
		})
	})
}

func handleSwaggerJSON(w http.ResponseWriter, r *http.Request) {
	data := strings.ReplaceAll(assets.SwaggerJSON, "\"host\": \"localhost\",", "\"host\": \"localhost:"+port+"\",")

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
		sentry.CurrentHub().Clone()
		defer sentry.Flush(5 * time.Second)
		defer sentry.Recover()

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
	bindAddressPtr := flag.String("bind-address", "localhost", "The IP address to bind the API and UI to (default 127.0.0.1)")
	parentPIDInt := flag.Int("parentpid", 0, "The process id (PID) of the proxy parent process")
	projectPathPtr := flag.String("project", "", "The path to the project to open")
	tempProjectPathPtr := flag.String("temp-project", "", "The path to the temporary project file to open")
	apiPortPtr := flag.Int("api-port", 10101, "The port for the API and UI to listen on (set to 0 for a random available port)")
	previewProxyPortPtr := flag.Int("preview-proxy-port", 10111, "The port for the preview proxy to listen on (set to 0 for a random available port)")
	errorReportingPtr := flag.Bool("error-reporting", false, "Whether to enable error reporting")

	flag.Parse()

	parentPID := int32(*parentPIDInt)

	if *projectPathPtr == "" {
		log.Fatal("A project path must be specified")
	}

	crashHandlingEnabled = *errorReportingPtr

	params := commandLineParameters{
		*apiKeyPtr,
		*bindAddressPtr,
		*projectPathPtr,
		*tempProjectPathPtr,
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

// ping godoc
// @Summary Healthcheck
// @Description returns a simple request to indicate that the service is up
// @Tags Misc
// @Security ApiKeyAuth
// @Success 200
// @Router /ping [get]
func ping(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func saveDatabasePaths(proj string, temp string) error {
	settings, err := proxy.GetSettings()
	if err != nil {
		return err
	}
	settings.OpenFile = proj
	settings.OpenTempFile = temp
	settings.OpenProcessPID = int32(os.Getpid())
	err = proxy.SaveSettings(settings)
	if err != nil {
		return err
	}

	return nil
}
