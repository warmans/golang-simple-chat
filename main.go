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
    connections     map[*Client]bool
    broadcast       chan []byte
    register        chan *Client
    unregister      chan *Client
}

func (h *Hub) NewConnection(conn *Client) *Client {

    //add hub to connection
    conn.hub = h

    //register with hub
    h.register <- conn

    //notify
    h.Broadcast("Peer Connected")

    return conn
}

func (h *Hub) CloseConnection(client *Client) {
    //remove connection
    h.unregister <- client

    //notify
    h.Broadcast("Peer Disconnected")
}

func (h *Hub) Broadcast(msg string) {
    h.broadcast <- []byte(msg)
}

func (h *Hub) Run() {
    for {
        select {

        //New Client
        case conn := <-h.register:
            h.connections[conn] = true

        //Client Disconnects
        case conn := <-h.unregister:
            if _, ok := h.connections[conn]; ok {
                delete(h.connections, conn)
                close(conn.send)
            }

        //New Message
        case msg := <-h.broadcast:
            for conn := range h.connections {
                select {
                case conn.send <- msg:
                default:
                    delete(h.connections, conn)
                    close(conn.send)
                }
            }
        }
    }
}

func NewHub() *Hub {
    return &Hub{
        broadcast:   make(chan []byte),
        register:    make(chan *Client),
        unregister:  make(chan *Client),
        connections: make(map[*Client]bool),

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

func (client *Client) LeaveHub() {
    client.hub.CloseConnection(client)
}

func (client *Client) Disconnect() {
    client.conn.Close()
}

func (client *Client) Reader() {

    defer client.Disconnect()

    for {
        _, msg, err := client.conn.ReadMessage()
        if err != nil {
            break;
        }
        client.hub.broadcast <- msg
    }
}

func (client *Client) Writer() {

    defer client.Disconnect()

    for msg := range client.send {
        if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
            break;
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

    sock, err := upgrader.Upgrade(w, r, nil);
    if err != nil {
        return
    }

    //open new client connection
    client := &Client{send: make(chan []byte, 256), conn: sock}

    //connect to hub
    handler.Hub.NewConnection(client)

    //unregister on connection close
    defer client.LeaveHub()

    //start writing
    go client.Writer()

    //start reading
    client.Reader()
}


//-----------------------------------------------
// Main
//-----------------------------------------------


func main() {

    var (
        addr      = flag.String("addr", ":8080", "http service address")
        static    = flag.String("static", "./static" , "path to static files")
    )

    flag.Parse()

    indexTemplate := template.Must(template.ParseFiles(filepath.Join(*static, "index.html")))

    hub := NewHub()
    go hub.Run()

    //static files
    http.HandleFunc("/", func(c http.ResponseWriter, req *http.Request) {
        indexTemplate.Execute(c, req.Host)
    })

    //websockets
    http.Handle("/ws", &WebsocketHandler{Hub: hub})

    if err := http.ListenAndServe(*addr, nil); err != nil {
        log.Fatal("HTTP Server failed: ", err)
    }
}
