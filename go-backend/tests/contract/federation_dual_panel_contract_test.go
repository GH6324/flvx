package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"go-backend/internal/auth"
	"go-backend/internal/security"
	"go-backend/internal/store/sqlite"
)

func TestFederationDualPanelMiddleExitAutoPortContract(t *testing.T) {
	providerSecret := "provider-contract-jwt"
	providerRouter, providerRepo := setupContractRouter(t, providerSecret)
	providerServer := httptest.NewServer(providerRouter)
	defer providerServer.Close()

	consumerSecret := "consumer-contract-jwt"
	consumerRouter, consumerRepo := setupContractRouter(t, consumerSecret)

	consumerAdminToken, err := auth.GenerateToken(1, "consumer-admin", 0, consumerSecret)
	if err != nil {
		t.Fatalf("generate consumer admin token: %v", err)
	}

	now := time.Now().UnixMilli()
	providerEntryNodeID := insertContractNode(t, providerRepo, "provider-entry", "198.51.100.11", "43000-43010", "provider-entry-secret", 1)
	providerMiddleNodeID := insertContractNode(t, providerRepo, "provider-middle", "198.51.100.12", "44000-44010", "provider-middle-secret", 1)
	providerExitNodeID := insertContractNode(t, providerRepo, "provider-exit", "198.51.100.13", "45000-45010", "provider-exit-secret", 1)

	entryShareID := insertPeerShare(t, providerRepo, &sqlite.PeerShare{
		Name:           "entry-share",
		NodeID:         providerEntryNodeID,
		Token:          "share-entry-token",
		PortRangeStart: 43000,
		PortRangeEnd:   43010,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
	})
	middleShareID := insertPeerShare(t, providerRepo, &sqlite.PeerShare{
		Name:           "middle-share",
		NodeID:         providerMiddleNodeID,
		Token:          "share-middle-token",
		PortRangeStart: 44000,
		PortRangeEnd:   44010,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
	})
	exitShareID := insertPeerShare(t, providerRepo, &sqlite.PeerShare{
		Name:           "exit-share",
		NodeID:         providerExitNodeID,
		Token:          "share-exit-token",
		PortRangeStart: 45000,
		PortRangeEnd:   45010,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
	})

	importRemoteNodeForContract(t, consumerRouter, consumerAdminToken, providerServer.URL, "share-entry-token")
	importRemoteNodeForContract(t, consumerRouter, consumerAdminToken, providerServer.URL, "share-middle-token")
	importRemoteNodeForContract(t, consumerRouter, consumerAdminToken, providerServer.URL, "share-exit-token")

	entryRemoteNodeID := queryRemoteNodeIDByToken(t, consumerRepo, "share-entry-token")
	middleRemoteNodeID := queryRemoteNodeIDByToken(t, consumerRepo, "share-middle-token")
	exitRemoteNodeID := queryRemoteNodeIDByToken(t, consumerRepo, "share-exit-token")

	stopMiddle := startMockNodeSession(t, providerServer.URL, "provider-middle-secret")
	defer stopMiddle()
	stopExit := startMockNodeSession(t, providerServer.URL, "provider-exit-secret")
	defer stopExit()

	createTunnel := func(name string) int64 {
		payload := map[string]interface{}{
			"name":   name,
			"type":   2,
			"flow":   99999,
			"status": 1,
			"inNodeId": []map[string]interface{}{
				{"nodeId": entryRemoteNodeID, "protocol": "tls", "strategy": "round"},
			},
			"chainNodes": [][]map[string]interface{}{
				{{"nodeId": middleRemoteNodeID, "protocol": "tls", "strategy": "round"}},
			},
			"outNodeId": []map[string]interface{}{
				{"nodeId": exitRemoteNodeID, "protocol": "tls", "strategy": "round"},
			},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal create payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", bytes.NewReader(body))
		req.Header.Set("Authorization", consumerAdminToken)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		consumerRouter.ServeHTTP(res, req)
		assertCode(t, res, 0)

		var tunnelID int64
		if err := consumerRepo.DB().QueryRow(`SELECT id FROM tunnel WHERE name = ? ORDER BY id DESC LIMIT 1`, name).Scan(&tunnelID); err != nil {
			t.Fatalf("query tunnel id (%s): %v", name, err)
		}
		if tunnelID <= 0 {
			t.Fatalf("invalid tunnel id for %s", name)
		}
		return tunnelID
	}

	firstTunnelID := createTunnel("dual-panel-middle-exit-1")

	assertTunnelPortInRange(t, consumerRepo, firstTunnelID, 2, middleRemoteNodeID, 44000, 44010)
	assertTunnelPortInRange(t, consumerRepo, firstTunnelID, 3, exitRemoteNodeID, 45000, 45010)

	assertCount(t, consumerRepo, `SELECT COUNT(1) FROM federation_tunnel_binding WHERE tunnel_id = ? AND status = 1`, firstTunnelID, 2)
	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ? AND status = 1 AND applied = 1`, middleShareID, 1)
	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ? AND status = 1 AND applied = 1`, exitShareID, 1)

	deleteBody, err := json.Marshal(map[string]interface{}{"id": firstTunnelID})
	if err != nil {
		t.Fatalf("marshal delete payload: %v", err)
	}
	deleteReq := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/delete", bytes.NewReader(deleteBody))
	deleteReq.Header.Set("Authorization", consumerAdminToken)
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteRes := httptest.NewRecorder()
	consumerRouter.ServeHTTP(deleteRes, deleteReq)
	assertCode(t, deleteRes, 0)

	assertCount(t, consumerRepo, `SELECT COUNT(1) FROM federation_tunnel_binding WHERE tunnel_id = ?`, firstTunnelID, 0)
	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ? AND status = 0`, middleShareID, 1)
	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ? AND status = 0`, exitShareID, 1)

	secondTunnelID := createTunnel("dual-panel-middle-exit-2")
	assertTunnelPortInRange(t, consumerRepo, secondTunnelID, 2, middleRemoteNodeID, 44000, 44010)
	assertTunnelPortInRange(t, consumerRepo, secondTunnelID, 3, exitRemoteNodeID, 45000, 45010)

	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ? AND status = 1 AND applied = 1`, middleShareID, 1)
	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ? AND status = 1 AND applied = 1`, exitShareID, 1)
	assertCount(t, providerRepo, `SELECT COUNT(1) FROM peer_share_runtime WHERE share_id = ?`, entryShareID, 0)
}

func insertContractNode(t *testing.T, repo *sqlite.Repository, name, ip, portRange, secret string, status int) int64 {
	t.Helper()
	now := time.Now().UnixMilli()
	res, err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, name, secret, ip, ip, "", portRange, "", "v1", 1, 1, 1, now, now, status, "[::]", "[::]", 0)
	if err != nil {
		t.Fatalf("insert node %s: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("node id %s: %v", name, err)
	}
	return id
}

func insertPeerShare(t *testing.T, repo *sqlite.Repository, share *sqlite.PeerShare) int64 {
	t.Helper()
	if share == nil {
		t.Fatalf("share is nil")
	}
	if err := repo.CreatePeerShare(share); err != nil {
		t.Fatalf("create peer share %s: %v", share.Name, err)
	}
	saved, err := repo.GetPeerShareByToken(share.Token)
	if err != nil {
		t.Fatalf("query peer share %s: %v", share.Name, err)
	}
	if saved == nil {
		t.Fatalf("peer share %s not found after create", share.Name)
	}
	return saved.ID
}

func importRemoteNodeForContract(t *testing.T, router http.Handler, adminToken, remoteURL, token string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"remoteUrl": remoteURL,
		"token":     token,
	})
	if err != nil {
		t.Fatalf("marshal import payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/node/import", bytes.NewReader(body))
	req.Header.Set("Authorization", adminToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assertCode(t, res, 0)
}

func queryRemoteNodeIDByToken(t *testing.T, repo *sqlite.Repository, token string) int64 {
	t.Helper()
	var id int64
	if err := repo.DB().QueryRow(`SELECT id FROM node WHERE is_remote = 1 AND remote_token = ? ORDER BY id DESC LIMIT 1`, token).Scan(&id); err != nil {
		t.Fatalf("query remote node by token %s: %v", token, err)
	}
	if id <= 0 {
		t.Fatalf("invalid remote node id for token %s", token)
	}
	return id
}

func assertTunnelPortInRange(t *testing.T, repo *sqlite.Repository, tunnelID int64, chainType int, nodeID int64, minPort int, maxPort int) {
	t.Helper()
	var port int
	err := repo.DB().QueryRow(`
		SELECT port
		FROM chain_tunnel
		WHERE tunnel_id = ? AND chain_type = ? AND node_id = ?
		LIMIT 1
	`, tunnelID, chainType, nodeID).Scan(&port)
	if err != nil {
		t.Fatalf("query tunnel=%d chainType=%d node=%d port: %v", tunnelID, chainType, nodeID, err)
	}
	if port < minPort || port > maxPort {
		t.Fatalf("expected port in range [%d,%d], got %d", minPort, maxPort, port)
	}
}

func assertCount(t *testing.T, repo *sqlite.Repository, query string, arg interface{}, expected int) {
	t.Helper()
	var got int
	if err := repo.DB().QueryRow(query, arg).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != expected {
		t.Fatalf("expected count %d, got %d (query: %s, arg: %v)", expected, got, query, arg)
	}
}

func startMockNodeSession(t *testing.T, baseURL string, nodeSecret string) func() {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse provider url: %v", err)
	}
	if strings.EqualFold(u.Scheme, "https") {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/system-info"
	q := u.Query()
	q.Set("type", "1")
	q.Set("secret", nodeSecret)
	q.Set("version", "v1")
	q.Set("http", "1")
	q.Set("tls", "1")
	q.Set("socks", "1")
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial mock node websocket: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}

			plain := raw
			var wrap struct {
				Encrypted bool   `json:"encrypted"`
				Data      string `json:"data"`
			}
			if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Encrypted && strings.TrimSpace(wrap.Data) != "" {
				crypto, cryptoErr := security.NewAESCrypto(nodeSecret)
				if cryptoErr == nil {
					if dec, decErr := crypto.Decrypt(wrap.Data); decErr == nil {
						plain = []byte(dec)
					}
				}
			}

			var cmd struct {
				Type      string `json:"type"`
				RequestID string `json:"requestId"`
			}
			if err := json.Unmarshal(plain, &cmd); err != nil {
				continue
			}
			if strings.TrimSpace(cmd.RequestID) == "" {
				continue
			}

			respType := fmt.Sprintf("%sResponse", cmd.Type)
			respBytes, err := json.Marshal(map[string]interface{}{
				"type":      respType,
				"success":   true,
				"message":   "OK",
				"requestId": cmd.RequestID,
			})
			if err != nil {
				continue
			}
			_ = conn.WriteMessage(websocket.TextMessage, respBytes)
		}
	}()

	return func() {
		_ = conn.Close()
		wg.Wait()
	}
}
