package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket" // ADDED
	"github.com/multiformats/go-multiaddr"
)

type RelayServer struct {
	host             host.Host
	ctx              context.Context
	cancel           context.CancelFunc
	startTime        time.Time
	mu               sync.RWMutex
	totalConnections int64
	peersConnected   map[peer.ID]time.Time
}

type Stats struct {
	PeerID           string   `json:"peer_id"`
	Uptime           string   `json:"uptime"`
	UptimeSeconds    float64  `json:"uptime_seconds"`
	ConnectedPeers   int      `json:"connected_peers"`
	TotalConnections int64    `json:"total_connections"`
	Addresses        []string `json:"addresses"`
	RelayAddresses   []string `json:"relay_addresses"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := NewRelayServer(ctx, port)
	if err != nil {
		fmt.Printf("Failed to create relay server: %v\n", err)
		os.Exit(1)
	}

	go startHTTPServer(port, server)
	printStartupInfo(server, port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	server.Stop()
}

func NewRelayServer(ctx context.Context, port string) (*RelayServer, error) {
	ctx, cancel := context.WithCancel(ctx)
	var privKey crypto.PrivKey
	privKeyHex := os.Getenv("PRIVATE_KEY")
	if privKeyHex != "" {
		keyBytes, _ := crypto.ConfigDecodeKey(privKeyHex)
		privKey, _ = crypto.UnmarshalPrivateKey(keyBytes)
	}
	if privKey == nil {
		privKey, _, _ = crypto.GenerateEd25519Key(rand.Reader)
	}

	// Listen on both TCP and WebSockets on the Render PORT
	tcpAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", port))
	wsAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%s/ws", port))

	h, err := libp2p.New(
		libp2p.ListenAddrs(tcpAddr, wsAddr),
		libp2p.Transport(websocket.New), // REQUIRED FOR RENDER
		libp2p.Identity(privKey),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableRelayService(relay.WithResources(relay.Resources{
			MaxReservations: 256,
			ReservationTTL:  time.Hour,
		})),
		libp2p.ForceReachabilityPublic(),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	server := &RelayServer{
		host: h, ctx: ctx, cancel: cancel, startTime: time.Now(),
		peersConnected: make(map[peer.ID]time.Time),
	}

	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, conn network.Conn) {
			server.mu.Lock()
			server.totalConnections++
			server.peersConnected[conn.RemotePeer()] = time.Now()
			server.mu.Unlock()
		},
		DisconnectedF: func(n network.Network, conn network.Conn) {
			server.mu.Lock()
			delete(server.peersConnected, conn.RemotePeer())
			server.mu.Unlock()
		},
	})

	return server, nil
}

func (s *RelayServer) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := Stats{
		PeerID:           s.host.ID().String(),
		Uptime:           time.Since(s.startTime).Round(time.Second).String(),
		ConnectedPeers:   len(s.peersConnected),
		TotalConnections: s.totalConnections,
	}
	for _, addr := range s.host.Addrs() {
		stats.RelayAddresses = append(stats.RelayAddresses, fmt.Sprintf("%s/p2p/%s", addr, s.host.ID()))
	}
	return stats
}

func (s *RelayServer) Stop() error { s.cancel(); return s.host.Close() }

func startHTTPServer(port string, server *RelayServer) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(server.GetStats())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Relay Online. PeerID: %s", server.host.ID())
	})
	http.ListenAndServe(":"+port, mux)
}

func printStartupInfo(server *RelayServer, port string) {
	fmt.Printf("🚀 Relay Started on port %s\nID: %s\n", port, server.host.ID())
}
