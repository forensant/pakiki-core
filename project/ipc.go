package project

// The IPC connection is modelled off the Chat websocket example: https://github.com/gorilla/websocket/tree/master/examples/chat

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	_ "embed"

	"github.com/gorilla/websocket"
)

//go:embed resources/debug_page.html
var debugPageHTML []byte

var ioHub *IOHub
var readableDatabase *gorm.DB

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

var (
	newline = []byte{'\n'}
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

func (h *IOHub) Run(projectPath string) {
	writableDatabase, err := gorm.Open(sqlite.Open(projectPath), &gorm.Config{})
	if err != nil {
		log.Fatal("Could not open the writable database: " + err.Error())
		return
	}
	initDatabase(writableDatabase)

	readableDatabase, err = gorm.Open(sqlite.Open(projectPath+"?mode=ro&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		log.Fatal("Could not open the read-only database: " + err.Error())
		return
	}

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

// writePump pumps messages from the hub to the websocket connection.
//
// A single goroutine is used per connection to ensure there's only a single writer for each connection
func (c *WebSocketClient) writePump(filter string) {
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

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			if !message.ShouldFilter(filter) {
				w.Write(marshalObject(message))
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
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

func IsValidOrigin(req *http.Request, apiToken string) bool {
	hostnameComponents := strings.Split(req.Host, ":")
	localhost := (hostnameComponents[0] == "localhost" || hostnameComponents[0] == "127.0.0.1")

	return apiToken != "" || localhost
}

func Notifications(hub *IOHub, apiToken string, w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return IsValidOrigin(r, apiToken)
	}
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Println(err)
		return
	}

	client := &WebSocketClient{hub: hub, conn: conn, send: make(chan BroadcastableObject, 255)}
	client.hub.register <- client

	go client.writePump(r.FormValue("filter"))
	go client.readPump()
}

func marshalObject(obj interface{}) []byte {
	b, _ := json.Marshal(obj)
	return b
}
