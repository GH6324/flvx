package handler

import (
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/sqlite"
)

func TestProcessFlowItemTracksPeerShareFlowAndEnforcesLimit(t *testing.T) {
	repo, err := sqlite.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer repo.Close()

	now := time.Now().UnixMilli()
	if err := repo.CreatePeerShare(&sqlite.PeerShare{
		Name:           "flow-share",
		NodeID:         1,
		Token:          "flow-share-token",
		MaxBandwidth:   3000,
		CurrentFlow:    1000,
		PortRangeStart: 32000,
		PortRangeEnd:   32010,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
	}); err != nil {
		t.Fatalf("create peer share: %v", err)
	}
	share, err := repo.GetPeerShareByToken("flow-share-token")
	if err != nil || share == nil {
		t.Fatalf("load peer share: %v", err)
	}

	if _, err := repo.DB().Exec(`
		INSERT INTO peer_share_runtime(id, share_id, node_id, reservation_id, resource_key, binding_id, role, chain_name, service_name, protocol, strategy, port, target, applied, status, created_time, updated_time)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 17, share.ID, share.NodeID, "res-17", "rk-17", "17", "exit", "", "fed_svc_17", "tls", "round", 32001, "", 1, 1, now, now); err != nil {
		t.Fatalf("insert peer_share_runtime: %v", err)
	}

	h := &Handler{repo: repo}
	h.processFlowItem(flowItem{N: "fed_svc_17", U: 1200, D: 900})

	updatedShare, err := repo.GetPeerShare(share.ID)
	if err != nil || updatedShare == nil {
		t.Fatalf("reload share: %v", err)
	}
	if updatedShare.CurrentFlow != 3100 {
		t.Fatalf("expected current_flow=3100, got %d", updatedShare.CurrentFlow)
	}

	runtime, err := repo.GetPeerShareRuntimeByID(17)
	if err != nil || runtime == nil {
		t.Fatalf("reload runtime: %v", err)
	}
	if runtime.Status != 0 {
		t.Fatalf("expected runtime status=0 after limit enforcement, got %d", runtime.Status)
	}
}
