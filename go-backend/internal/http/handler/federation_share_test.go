package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestFederationShareCreateRejectsInvalidAllowedIPs(t *testing.T) {
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
	`, "local-share-node", "local-share-secret", "10.20.30.40", "10.20.30.40", "", "21000-21010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0, 0, "", "", "")
	if err != nil {
		t.Fatalf("insert local node: %v", err)
	}
	localNodeID, err := insertRes.LastInsertId()
	if err != nil {
		t.Fatalf("get local node id: %v", err)
	}

	body, err := json.Marshal(createPeerShareRequest{
		Name:           "local-node-share",
		NodeID:         localNodeID,
		MaxBandwidth:   0,
		ExpiryTime:     0,
		PortRangeStart: 21000,
		PortRangeEnd:   21010,
		AllowedIPs:     "bad-ip-entry",
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
	if !strings.Contains(payload.Msg, "Invalid allowed IP or CIDR") {
		t.Fatalf("expected invalid IP message, got %q", payload.Msg)
	}

	var shareCount int
	if err := repo.DB().QueryRow(`SELECT COUNT(1) FROM peer_share WHERE node_id = ?`, localNodeID).Scan(&shareCount); err != nil {
		t.Fatalf("query peer_share count: %v", err)
	}
	if shareCount != 0 {
		t.Fatalf("expected no share rows for node, got %d", shareCount)
	}
}

func TestAuthPeerAllowedIPs(t *testing.T) {
	repo, err := sqlite.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	h := New(repo, "test-jwt-secret")
	now := time.Now().UnixMilli()

	tests := []struct {
		name        string
		allowedIPs  string
		remoteAddr  string
		xff         string
		wantAllowed bool
	}{
		{
			name:        "exact ip allowed",
			allowedIPs:  "203.0.113.10",
			remoteAddr:  "203.0.113.10:23456",
			wantAllowed: true,
		},
		{
			name:        "cidr allowed",
			allowedIPs:  "203.0.113.0/24",
			remoteAddr:  "203.0.113.11:23456",
			wantAllowed: true,
		},
		{
			name:        "trusted proxy xff allowed",
			allowedIPs:  "198.51.100.20",
			remoteAddr:  "172.20.0.3:34567",
			xff:         "198.51.100.20, 172.20.0.3",
			wantAllowed: true,
		},
		{
			name:        "non whitelisted ip denied",
			allowedIPs:  "203.0.113.10",
			remoteAddr:  "203.0.113.99:23456",
			wantAllowed: false,
		},
	}

	for idx, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := fmt.Sprintf("share-token-%d", idx)
			if err := repo.CreatePeerShare(&sqlite.PeerShare{
				Name:           "share-" + tt.name,
				NodeID:         1,
				Token:          token,
				PortRangeStart: 10000,
				PortRangeEnd:   10010,
				IsActive:       1,
				CreatedTime:    now,
				UpdatedTime:    now,
				AllowedIPs:     tt.allowedIPs,
			}); err != nil {
				t.Fatalf("create peer share: %v", err)
			}

			nextCalled := false
			wrapped := h.authPeer(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				response.WriteJSON(w, response.OKEmpty())
			})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/connect", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			req.RemoteAddr = tt.remoteAddr

			res := httptest.NewRecorder()
			wrapped(res, req)

			var payload response.R
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if tt.wantAllowed {
				if !nextCalled {
					t.Fatalf("expected next handler to be called")
				}
				if payload.Code != 0 {
					t.Fatalf("expected code 0, got %d (%s)", payload.Code, payload.Msg)
				}
				return
			}

			if nextCalled {
				t.Fatalf("expected next handler not to be called")
			}
			if payload.Code != 403 {
				t.Fatalf("expected code 403, got %d (%s)", payload.Code, payload.Msg)
			}
			if payload.Msg != "IP not allowed" {
				t.Fatalf("expected IP rejection message, got %q", payload.Msg)
			}
		})
	}
}
