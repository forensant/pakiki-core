package project

// The IPC connection is modelled off the Chat websocket example: https://github.com/gorilla/websocket/tree/master/examples/chat

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	_ "embed"

	"github.com/gorilla/websocket"
)

//go:embed resources/debug_page.html
var debugPageHTML []byte

var autosaveTimer *time.Timer
var decompressedDBPath string
var projectMutex sync.Mutex
var ioHub *IOHub
var projectPath string
var readableDatabase *gorm.DB
var writableDatabase *gorm.DB

type DBRecord interface {
	WriteToDatabase(db *gorm.DB)
}

type BroadcastableObject interface {
	ShouldFilter(filter string) bool
}

type IOHub struct {
	// registered clients
	clients map[*WebSocketClient]bool

	// inbound messages from the clients
	broadcast chan BroadcastableObject

	// register requests from the clients
	register chan *WebSocketClient

	// unregister requests from clients
	unregister chan *WebSocketClient

	// writes a given record into the database
	databaseWriter chan DBRecord
}

const (
	// time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// time allowed to read the next pong message from teh peer
	pongWait = 60 * time.Second

	// send pings to the peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// maximum message size allowed from peer
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type WebSocketClient struct {
	hub *IOHub

	// websocket conneciton
	conn *websocket.Conn

	// buffered channel of outbound messages
	send chan BroadcastableObject
}

func autosave() {
	fmt.Printf("Automatically saving project\n")
	saveProject()
	fmt.Printf("Project automatically saved\n")

	autosaveTimer = time.AfterFunc(time.Minute*5, autosave)
}

func CloseProject() {
	autosaveTimer.Stop()
	fmt.Printf("Saving and closing project - this could take a while for large projects\n")
	saveProject()
	err := os.Remove(decompressedDBPath)
	if err != nil {
		fmt.Printf("Failed removing the temporary database: %s, path: %s\n", err.Error(), decompressedDBPath)
	}
	fmt.Printf("Project closed\n")
}

func Debug(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Add("Content-Type", "text/html")
	w.Write(debugPageHTML)
}

func marshalObject(obj interface{}) []byte {
	b, _ := json.Marshal(obj)
	return b
}

func NewIOHub() *IOHub {
	ioHub = &IOHub{
		broadcast:      make(chan BroadcastableObject),
		register:       make(chan *WebSocketClient),
		unregister:     make(chan *WebSocketClient),
		clients:        make(map[*WebSocketClient]bool),
		databaseWriter: make(chan DBRecord),
	}

	return ioHub
}

// Notifications godoc
// @Summary Stream updates
// @Description websocket endpoint to stream data as it is inserted/modified
// @Tags Misc
// @Produce json
// @Security ApiKeyAuth
// @Param objectfieldfilter query string false "JSON object (key:value) where the returned objects will be filtered by the values"
// @Param filter query string false "additional filter to apply to the objects (behaviour is object dependent)"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /notifications [get]
func Notifications(hub *IOHub, apiToken string, w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool {
		key := r.FormValue("api_key")
		return key == apiToken
	}
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Println(err)
		return
	}

	client := &WebSocketClient{hub: hub, conn: conn, send: make(chan BroadcastableObject, 255)}
	client.hub.register <- client

	objectFieldFilter := make(map[string]string)
	fieldFilterVal := r.FormValue("objectfieldfilter")

	if fieldFilterVal != "" {
		err := json.Unmarshal([]byte(fieldFilterVal), &objectFieldFilter)
		if err != nil {
			log.Printf("Error when unmarshalling objectfieldfilter: %s\n", err)
		}
	}

	go client.writePump(r.FormValue("filter"), objectFieldFilter)
	go client.readPump()
}

// readPump pumps messages from the websocket connection to the hub
//
// A single goroutine is used by each connection to ensure that there is only a single reader per connection.
func (c *WebSocketClient) readPump() {
	defer func() {

		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		// at this point we're not doing anything with the messages over the websocket connection
	}
}

func (h *IOHub) Run(p string, tempPath string) (*gorm.DB, string) {
	var err error
	projectPath = p

	if tempPath != "" && fileExists(tempPath) {
		decompressedDBPath = tempPath
	} else {
		decompressedDBPath, err = decompressDatabase(projectPath, tempPath)
		if err != nil {
			log.Fatal("Error when decompressing the project: ", err.Error())
		}
	}

	writableDatabase, err = gorm.Open(sqlite.Open(decompressedDBPath), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		log.Fatal("Could not open the writable database: " + err.Error())
		return nil, ""
	}
	initDatabase(writableDatabase)

	readableDatabase, err = gorm.Open(sqlite.Open(decompressedDBPath+"?mode=ro&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		log.Fatal("Could not open the read-only database: " + err.Error())
		return nil, ""
	}

	autosaveTimer = time.AfterFunc(time.Minute*5, autosave)

	go func() {
		for {
			select {
			case client := <-h.register:
				h.clients[client] = true
			case client := <-h.unregister:
				if _, ok := h.clients[client]; ok {
					delete(h.clients, client)
					close(client.send)
				}
			case message := <-h.broadcast:
				for client := range h.clients {
					select {
					case client.send <- message:
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
			case record := <-h.databaseWriter:
				record.WriteToDatabase(writableDatabase)
			}

		}
	}()

	return readableDatabase, decompressedDBPath
}

func saveProject() {
	projectMutex.Lock()
	err := compressDatabase(writableDatabase, projectPath)
	if err != nil {
		fmt.Printf("Failed compressing the project: %s\n", err.Error())
	}
	projectMutex.Unlock()
}

func shouldFilterByFields(objectFieldFilter map[string]string, message interface{}) bool {
	messageValues := reflect.ValueOf(message).Elem()

	for key, value := range objectFieldFilter {
		field := messageValues.FieldByName(key)

		if !field.IsValid() {
			return true
		}

		if field.Kind() != reflect.String {
			fmt.Printf("Filtering notification, field %s was not a string.\n", key)
			return true
		}

		if field.String() != value {
			return true
		}
	}

	return false
}

// writePump pumps messages from the hub to the websocket connection.
//
// A single goroutine is used per connection to ensure there's only a single writer for each connection
func (c *WebSocketClient) writePump(filter string, objectFieldFilter map[string]string) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if !message.ShouldFilter(filter) && !shouldFilterByFields(objectFieldFilter, message) {
				w, err := c.conn.NextWriter(websocket.TextMessage)
				if err != nil {
					return
				}

				w.Write(marshalObject(message))

				if err := w.Close(); err != nil {
					return
				}
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
