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
    clients     map[*Client]bool
    broadcast       chan []byte
    register        chan *Client
    unregister      chan *Client
}

func (h *Hub) Join(conn *Client) {
    //add hub to connection
    conn.hub = h

    //register with hub
    h.register <- conn
}

func (h *Hub) CloseConnection(client *Client) {
    //remove connection
    h.unregister <- client
}

func (h *Hub) Run() {
    for {
        select {

        //new client
        case conn := <-h.register:
            //do add
            h.clients[conn] = true
            log.Print("client connected")

        //client disconnects
        case conn := <-h.unregister:
            if _, ok := h.clients[conn]; ok {
                //do remove
                delete(h.clients, conn)
                close(conn.send)
                log.Print("client left")
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
        broadcast:   make(chan []byte),
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

func (client *Client) LeaveHub() {
    client.hub.CloseConnection(client)
}

func (client *Client) Disconnect() {
    client.LeaveHub()
    client.conn.Close()
}

func (client *Client) Send(msg []byte) {
    select {
    case  client.send <- msg:
    default:
        //seems the client's buffer is full - they're probably dead
        log.Print("Client buffer full")
        client.Disconnect()
    }
}

func (client *Client) AcceptMessages() {

    defer client.Disconnect()

    for {
        _, msg, err := client.conn.ReadMessage()
        if err != nil {
            log.Print(err)
            break;
        }
        client.hub.broadcast <- msg
    }
}

func (client *Client) RelayMessages() {

    defer client.Disconnect()

    for msg := range client.send {
        if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
            log.Print(err);
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
    handler.Hub.Join(client)

    //unregister on connection close
    defer client.Disconnect()

    //start relaying messages to client
    go client.RelayMessages()

    //start accepting messages from
    client.AcceptMessages()
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
