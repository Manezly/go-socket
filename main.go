package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

type Client struct {
        conn *websocket.Conn
        mu   sync.Mutex
}

type Server struct {
        zones map[string]map[*Client]bool
        mu    sync.Mutex
        uuids map[string]*Client
}

func NewServer() *Server {
        return &Server{
                zones: make(map[string]map[*Client]bool),
                uuids: make(map[string]*Client),
        }
}

func (s *Server) handleWS(ws *websocket.Conn) {
        r := ws.Request()
        zoneParam := r.URL.Query().Get("zones")
        zones := strings.Split(zoneParam, ",")

        clientUUID := r.URL.Query().Get("uuid")

        if clientUUID == "" {
                clientUUID = uuid.New().String()
                fmt.Println("Generated new UUID for client:", clientUUID)

                if err := websocket.Message.Send(ws, clientUUID); err != nil {
                        fmt.Println("Error sending UUID to client:", err)
                        return
                }
        } else {
                fmt.Println("Client connected with UUID:", clientUUID)
        }

        client := &Client{conn: ws}

        s.mu.Lock()
        defer s.mu.Unlock()

        if existingClient, ok := s.uuids[clientUUID]; ok {
                for zone := range s.zones {
                        if _, exists := s.zones[zone][existingClient]; exists {
                                delete(s.zones[zone], existingClient)
                                if len(s.zones[zone]) == 0 {
                                        delete(s.zones, zone)
                                }
                        }
                }
        }

        s.uuids[clientUUID] = client

        for _, zone := range zones {
                if _, ok := s.zones[zone]; !ok {
                        s.zones[zone] = make(map[*Client]bool)
                }
                s.zones[zone][client] = true
                fmt.Printf("Added connection to zone: %s\n", zone)
        }

        s.readLoop(client, zones, clientUUID)
}

func (s *Server) handleAdminMessage(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

        if r.Method == http.MethodOptions {
                return
        }

        if r.Method != http.MethodPost {
                http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
                return
        }

        var msg struct {
                Zone    string `json:"zone"`
                Message string `json:"message"`
        }

        if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
                http.Error(w, "Invalid request body", http.StatusBadRequest)
                return
        }

        s.broadcast([]byte(msg.Message), msg.Zone)
        fmt.Fprintf(w, "Message sent to zone %s", msg.Zone)
}

func (s *Server) broadcast(b []byte, zone string) {
	fmt.Println("Trying to send message...", zone)

	// s.mu.Lock()
	connections, ok := s.zones[zone]
	connectionsCopy := make(map[*Client]bool, len(connections))
	for client := range connections {
			connectionsCopy[client] = true
	}
	s.mu.Unlock()

	if zone != "" {
			if ok {
					fmt.Printf("Found %d connections in zone %s\n", len(connectionsCopy), zone)
					for client := range connectionsCopy {
							go func(client *Client) {
									client.mu.Lock()
									defer client.mu.Unlock()
									if err := websocket.Message.Send(client.conn, string(b)); err != nil {
											fmt.Println("Error sending message to client:", err)
									} else {
											fmt.Printf("Message sent to client in zone %s\n", zone)
									}
							}(client)
					}
			} else {
					fmt.Printf("No connections found for zone: %s\n", zone)
			}
	}
}

func (s *Server) readLoop(client *Client, zones []string, clientUUID string) {
        buf := make([]byte, 1024)
        for {
                n, err := client.conn.Read(buf) // Read from client.conn
                if err != nil {
                        if err == io.EOF {
                                break
                        }
                        fmt.Println("read error:", err)
                        continue
                }
                msg := buf[:n]
                fmt.Println("Received message from client:", string(msg))
                // ... handle the message ...
        }

        s.mu.Lock()
        defer s.mu.Unlock()

        for _, zone := range zones {
                if _, ok := s.zones[zone][client]; ok {
                        delete(s.zones[zone], client)
                        if len(s.zones[zone]) == 0 {
                                delete(s.zones, zone)
                        }
                }
        }
        delete(s.uuids, clientUUID)
        fmt.Println("Client disconnected and removed from zones.")
        client.conn.Close() // Close the websocket connection. Important!
}

func main() {
        server := NewServer()
        http.Handle("/ws", websocket.Handler(server.handleWS))
        http.HandleFunc("/admin/message", server.handleAdminMessage)
        http.ListenAndServe(":3000", nil)
}