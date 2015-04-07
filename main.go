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
    connections     map[*Connection]bool
    broadcast       chan []byte
    register        chan *Connection
    unregister      chan *Connection
}

func (h *Hub) NewConnection(conn *Connection) *Connection {

    //add hub to connection
    conn.hub = h

    //register with hub
    h.register <- conn

    //notify
    h.Broadcast("Peer Connected")

    return conn
}

func (h *Hub) CloseConnection(conn *Connection) {
    //remove connection
    conn.hub.unregister <- conn

    //notify
    conn.hub.Broadcast("Peer Disconnected")
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
        register:    make(chan *Connection),
        unregister:  make(chan *Connection),
        connections: make(map[*Connection]bool),

    }
}

//-----------------------------------------------
// Connection
//-----------------------------------------------

type Connection struct {

    sock *websocket.Conn

    send chan []byte

    hub *Hub
}

func (conn *Connection) Close() {
    conn.hub.CloseConnection(conn)
}

func (conn *Connection) Reader() {

    defer conn.sock.Close()

    for {
        _, msg, err := conn.sock.ReadMessage()
        if err != nil {
            break;
        }
        conn.hub.broadcast <- msg
    }
}

func (conn *Connection) Writer() {

    defer conn.sock.Close()

    for msg := range conn.send {
        if err := conn.sock.WriteMessage(websocket.TextMessage, msg); err != nil {
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
    conn := &Connection{send: make(chan []byte, 256), sock: sock}

    //connect to hub
    handler.Hub.NewConnection(conn)

    //unregister on connection close
    defer conn.Close()

    //start writing
    go conn.Writer()

    //start reading
    conn.Reader()
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
