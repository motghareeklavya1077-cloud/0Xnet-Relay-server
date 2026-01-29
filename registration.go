package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
)

func registerHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "unknown-device"
	}

	id := uuid.New().String()

	device := &Device{
		ID:        id,
		Name:      name,
		Connected: false,
	}

	mu.Lock()
	devices[id] = device
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)

	log.Println("📌 Registered device:", name, id)
}
