package main

import (
	"encoding/json"
    "fmt"
    "net/http"

    "github.com/gorilla/websocket"
    uuid "github.com/satori/go.uuid"
)
// 客戶端管理
type ClientManager struct {
	clients    map[*Client]bool // 客戶端map儲存並管理所有的長連線client,線上的為true, 不在的為false
	broadcast  chan []byte 	    // web端傳送來的訊息,用broadcast接收,並分發給所有的client
	register   chan *Client 	// 新建立的長連線client
	unregister chan *Client     // 新登出的長連線client
}

// 客戶端client
type Client struct {
	id     string          // 使用者id
	socket *websocket.Conn // 連線的socket
	send   chan []byte     // 傳送的訊息
}

// message格式化成json
type Message struct {
	Sender    string `json:"sender,omitempty"`    // 傳送者
	Recipient string `json:"recipient,omitempty"` // 接收者
	Content   string `json:"content,omitempty"`   // 內容
}

// 建立客戶端管理者
var manager = ClientManager{
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[*Client]bool),
}

func (manager *ClientManager) start() {
	for {
		select {
		//如果有新的連線接入,就通過channel把連線傳遞給conn
		case conn := <- manager.register:
			manager.clients[conn] = true // 把客戶端的連線設定為true
			jsonMessage, _ := json.Marshal(&Message{Content: "/A new socket has connected."}) // 把返回連線成功的訊息json格式化
			manager.send(jsonMessage, conn) // 呼叫客戶端的send方法, 傳送訊息
		// 連線斷開
		case conn := <- manager.unregister:
			//判斷連線的狀態，如果是true, 就關閉send，刪除連線client的值
			if _, ok := manager.clients[conn]; ok {
				close(conn.send)
				delete(manager.clients, conn)
				jsonMessage, _ := json.Marshal(&Message{Content: "/A socket has disconnected."})
				manager.send(jsonMessage, conn)
			}
		// 廣播
		case message := <- manager.broadcast:
			//遍歷已經連線的客戶端，把訊息傳送給他們
			for conn := range manager.clients {
				select {
				case conn.send <- message:
				default:
					close(conn.send)
					delete(manager.clients, conn)
				}
			}
		}
	}
}

// 定義客戶端管理的send方法
func (manager *ClientManager) send(message []byte, ignore *Client) {
	for conn := range manager.clients {
		//不給遮蔽的連線傳送訊息
		if conn != ignore {
			conn.send <- message
		}
	}
}

//定義客戶端結構體的read方法
func (c *Client) read() {
	defer func() {
		manager.unregister <- c
		c.socket.Close()
	}()

	for {
		//讀取訊息
		_, message, err := c.socket.ReadMessage()
		//如果有錯誤資訊，就登出這個連線然後關閉
		if err != nil {
			manager.unregister <- c
			c.socket.Close()
			break
		}
		//如果沒有錯誤資訊就把資訊放入broadcast
		jsonMessage, _ := json.Marshal(&Message{Sender: c.id, Content: string(message)})
		manager.broadcast <- jsonMessage
	}
}

func (c *Client) write() {
	defer func() {
		c.socket.Close()
	}()
	
	for {
		select {
		//從send裡讀訊息
		case message, ok := <- c.send:
			//如果沒有訊息
			if !ok {
				c.socket.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			//有訊息就寫入，傳送給web端
			c.socket.WriteMessage(websocket.TextMessage, message)
		}
	}
}

func main() {
	fmt.Println("Starting application...")
	go manager.start() //開一個goroutine執行開始程式
	http.HandleFunc("/ws", wsPage) //註冊預設路由為 /ws ，並使用wsHandler這個方法
	http.ListenAndServe(":12345", nil) //監聽本地的12345埠
}

func wsPage(res http.ResponseWriter, req *http.Request) {
	//將http協議升級成websocket協議
	conn, error := (&websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}).Upgrade(res, req, nil)
	if error != nil {
		http.NotFound(res, req)
		return
	}
	//每一次連線都會新開一個client，client.id通過uuid生成保證每次都是不同的
	u, _ :=  uuid.NewV4()
	id := u.String()
	client := &Client{
		id: id,
		socket: conn, 
		send: make(chan []byte),
	}
	//註冊一個新的連結
	manager.register <- client
	go client.read() //啟動協程收web端傳過來的訊息
	go client.write() //啟動協程把訊息返回給web端
}