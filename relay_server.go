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
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
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
	RelayAddresses   []string `json:"relay_addresses"`
}

func main() {
	renderPort := os.Getenv("PORT")
	if renderPort == "" {
		renderPort = "8080"
	}
	internalHttpPort := "9090"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := NewRelayServer(ctx, renderPort)
	if err != nil {
		log.Fatalf("Failed to start relay: %v", err)
	}

	go startHTTPServer(internalHttpPort, server)
	
	fmt.Println("🚀 0Xnet Relay Deployment Version")
	fmt.Println("Peer ID:", server.host.ID())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	server.Stop()
}

func NewRelayServer(ctx context.Context, p2pPort string) (*RelayServer, error) {
	ctx, cancel := context.WithCancel(ctx)

	var privKey crypto.PrivKey
	if privKeyHex := os.Getenv("PRIVATE_KEY"); privKeyHex != "" {
		keyBytes, _ := crypto.ConfigDecodeKey(privKeyHex)
		privKey, _ = crypto.UnmarshalPrivateKey(keyBytes)
	} else {
		privKey, _, _ = crypto.GenerateEd25519Key(rand.Reader)
		kb, _ := crypto.MarshalPrivateKey(privKey)
		fmt.Printf("\n🔑 NEW PRIVATE_KEY: %s\n\n", crypto.ConfigEncodeKey(kb))
	}

	tcpAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", p2pPort))
	wsAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%s/ws", p2pPort))

	h, err := libp2p.New(
		libp2p.ListenAddrs(tcpAddr, wsAddr),
		libp2p.Transport(websocket.New),
		libp2p.Identity(privKey),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		// CRITICAL: Tells relay to ignore private IP detection and allow reservations
		libp2p.ForceReachabilityPublic(),
		// CRITICAL: Stops the relay from banning peers for multiple connection attempts
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.EnableRelayService(
			relay.WithResources(relay.Resources{
				MaxReservations:        1024,
				MaxReservationsPerPeer: 50,
				MaxReservationsPerIP:   50,
				ReservationTTL:         time.Hour,
			}),
		),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	server := &RelayServer{
		host:           h,
		ctx:            ctx,
		cancel:         cancel,
		startTime:      time.Now(),
		peersConnected: make(map[peer.ID]time.Time),
	}

	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, c network.Conn) {
			server.mu.Lock()
			server.totalConnections++
			server.peersConnected[c.RemotePeer()] = time.Now()
			server.mu.Unlock()
			log.Printf("✅ Peer Connected: %s", c.RemotePeer())
		},
		DisconnectedF: func(n network.Network, c network.Conn) {
			server.mu.Lock()
			delete(server.peersConnected, c.RemotePeer())
			server.mu.Unlock()
			log.Printf("❌ Peer Disconnected: %s", c.RemotePeer())
		},
	})

	return server, nil
}

func startHTTPServer(port string, server *RelayServer) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "0Xnet Relay Online\nPeerID: %s\n", server.host.ID())
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(server.GetStats())
	})
	log.Printf("Internal HTTP stats running on :%s", port)
	http.ListenAndServe(":"+port, mux)
}

func (s *RelayServer) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := Stats{
		PeerID:           s.host.ID().String(),
		Uptime:           time.Since(s.startTime).Round(time.Second).String(),
		UptimeSeconds:    time.Since(s.startTime).Seconds(),
		ConnectedPeers:   len(s.peersConnected),
		TotalConnections: s.totalConnections,
	}
	for _, addr := range s.host.Addrs() {
		stats.RelayAddresses = append(stats.RelayAddresses, fmt.Sprintf("%s/p2p/%s", addr, s.host.ID()))
	}
	return stats
}

func (s *RelayServer) Stop() error {
	s.cancel()
	return s.host.Close()
}