package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

type RelayMessage struct {
	To   string `json:"to"`
	Data string `json:"data"`
}

var (
	devices     = make(map[string]*Device)
	connections = make(map[string]*websocket.Conn)
	mu          sync.Mutex
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // OK for now (tighten later)
	},
}

func main() {
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/devices", devicesHandler)
	http.HandleFunc("/ws", websocketHandler)

	log.Println("🚀 Relay server running on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
