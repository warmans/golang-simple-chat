package main

import (
    "github.com/gorilla/websocket"
    "net/http"
    "flag"
    "text/template"
    "path/filepath"
    "log"
)

var upgrader = &websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

//-----------------------------------------------
// Hub
//-----------------------------------------------

type Hub struct {
    clients         map[*Client]bool
    broadcast       chan []byte
    register        chan *Client
    unregister      chan *Client
}

func (h *Hub) Register(conn *Client) {
    h.register <- conn
}

func (h *Hub) Unregister(client *Client) {
    h.unregister <- client
}

func (h *Hub) Broadcast(msg []byte) {
    h.broadcast <- msg
}

func (h *Hub) Run() {
    for {
        select {

        //new client
        case conn := <-h.register:
            h.clients[conn] = true
            //important: never block Run
            go h.Broadcast([]byte("client joined"))

        //client disconnects
        case conn := <-h.unregister:
            if _, ok := h.clients[conn]; ok {
                //do remove
                delete(h.clients, conn)
                close(conn.send)
                go h.Broadcast([]byte("client left"))
            }

        //new message
        case msg := <-h.broadcast:
            for client := range h.clients {
                client.Send(msg)
            }
        }
    }
}

func NewHub() *Hub {
    return &Hub{
        broadcast:   make(chan []byte, 64),
        register:    make(chan *Client),
        unregister:  make(chan *Client),
        clients:     make(map[*Client]bool),
    }
}

//-----------------------------------------------
// Client
//-----------------------------------------------

type Client struct {
    conn *websocket.Conn
    send chan []byte
    hub *Hub
}

func (client *Client) JoinHub(hub *Hub) {
    client.hub = hub
    client.hub.Register(client)
}

func (client *Client) LeaveHub() {
    if (client.hub != nil) {
        client.hub.Unregister(client)
    }
}

func (client *Client) Close() {
    client.LeaveHub()
    client.conn.Close()
}

func (client *Client) Send(msg []byte) {
    select {
    case  client.send <- msg:
    default:
        log.Print("client send buffer is full - they're probably dead so they've been disconnected")
        client.Close()
    }
}

// AcceptMessages accepts messages from client and re-broadcasts to other clients
func (client *Client) AcceptMessages() {

    defer client.Close()

    for {
        _, msg, err := client.conn.ReadMessage()
        if err != nil {
            break
        }
        client.hub.Broadcast(msg)
    }
}

// RelayMessages sends messages from other clients to client
func (client *Client) RelayMessages() {

    defer client.Close()

    for msg := range client.send {
        if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
            break
        }
    }
}

//-----------------------------------------------
// HTTP Handler
//-----------------------------------------------

type WebsocketHandler struct {
    Hub *Hub
}

func (handler *WebsocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Print("socket upgrade failed: ", err)
        return
    }

    //open new client connection
    client := &Client{send: make(chan []byte, 256), conn: conn}

    //join the hub
    client.JoinHub(handler.Hub)

    //unregister on connection close
    defer client.Close()

    //start relaying messages to client
    go client.RelayMessages()

    //start accepting messages from
    client.AcceptMessages()
}

//-----------------------------------------------
// Main
//-----------------------------------------------

func main() {

    flag.Parse()

    indexTemplate := template.Must(template.ParseFiles(filepath.Join(*flag.String("static", "./static" , "path to static files"), "index.html")))

    hub := NewHub()
    go hub.Run()

    //static files
    http.HandleFunc("/", func(c http.ResponseWriter, req *http.Request) {
        indexTemplate.Execute(c, req.Host)
    })

    //websockets
    http.Handle("/ws", &WebsocketHandler{Hub: hub})

    if err := http.ListenAndServe(*flag.String("addr", ":8080", "http service address"), nil); err != nil {
        log.Fatal("HTTP Server failed: ", err)
    }
}
