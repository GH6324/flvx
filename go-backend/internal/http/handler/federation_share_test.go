package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/sqlite"
)

func TestFederationShareCreateRejectsRemoteNode(t *testing.T) {
	repo, err := sqlite.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	h := New(repo, "test-jwt-secret")
	now := time.Now().UnixMilli()

	insertRes, err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx, is_remote, remote_url, remote_token, remote_config)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "remote-share-node", "remote-share-secret", "10.10.10.1", "10.10.10.1", "", "20000-20010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0, 1, "http://peer.example", "peer-token", `{"shareId":1}`)
	if err != nil {
		t.Fatalf("insert remote node: %v", err)
	}
	remoteNodeID, err := insertRes.LastInsertId()
	if err != nil {
		t.Fatalf("get remote node id: %v", err)
	}

	body, err := json.Marshal(createPeerShareRequest{
		Name:           "remote-node-share",
		NodeID:         remoteNodeID,
		MaxBandwidth:   0,
		ExpiryTime:     0,
		PortRangeStart: 20000,
		PortRangeEnd:   20010,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/share/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	h.federationShareCreate(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	var payload response.R
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != -1 {
		t.Fatalf("expected response code -1, got %d", payload.Code)
	}
	if payload.Msg != "Only local nodes can be shared" {
		t.Fatalf("expected rejection message %q, got %q", "Only local nodes can be shared", payload.Msg)
	}

	var shareCount int
	if err := repo.DB().QueryRow(`SELECT COUNT(1) FROM peer_share WHERE node_id = ?`, remoteNodeID).Scan(&shareCount); err != nil {
		t.Fatalf("query peer_share count: %v", err)
	}
	if shareCount != 0 {
		t.Fatalf("expected no share rows for remote node, got %d", shareCount)
	}
}
