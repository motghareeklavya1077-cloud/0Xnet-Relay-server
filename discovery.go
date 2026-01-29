package main

import (
	"encoding/json"
	"net/http"
)

func devicesHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	list := make([]*Device, 0, len(devices))
	for _, d := range devices {
		list = append(list, d)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}
