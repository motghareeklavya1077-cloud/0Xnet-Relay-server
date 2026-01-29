package main

import (
	"log"
	"net/http"
)

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing device id", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	mu.Lock()
	connections[id] = conn
	if d, ok := devices[id]; ok {
		d.Connected = true
	}
	mu.Unlock()

	log.Println("🔌 Device connected:", id)

	for {
		var msg RelayMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}

		mu.Lock()
		targetConn, ok := connections[msg.To]
		mu.Unlock()

		if ok {
			targetConn.WriteJSON(map[string]string{
				"from": id,
				"data": msg.Data,
			})
		}
	}

	mu.Lock()
	delete(connections, id)
	if d, ok := devices[id]; ok {
		d.Connected = false
	}
	mu.Unlock()

	log.Println("❌ Device disconnected:", id)
}
