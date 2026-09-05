// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"nvpair-shared/noderec"
	"nvpair-shared/schedulerwire"
)

func TestTelemetryCacheAgesObservations(t *testing.T) {
	cache := newTelemetryCache()
	receivedAt := time.Unix(1_700_000_000, 0)
	input := noderec.NodeTelemetry{
		HostUUID:          "node-a",
		GPUUtilizationPct: 84,
		TelemetryValid:    true,
		MSSince:           137,
	}
	projected, ok := cache.Upsert(sourceScanner, input, receivedAt)
	if !ok || projected.MSSince != 137 {
		t.Fatalf("initial projection = %+v, ok=%v", projected, ok)
	}
	snapshot := cache.Snapshot(receivedAt.Add(250 * time.Millisecond))
	if len(snapshot) != 1 {
		t.Fatalf("snapshot length = %d, want 1", len(snapshot))
	}
	if snapshot[0].MSSince != 387 {
		t.Fatalf("aged msSince = %d, want 387", snapshot[0].MSSince)
	}
	if snapshot[0].GPUUtilizationPct != 84 {
		t.Fatalf("cached utilization = %d, want 84", snapshot[0].GPUUtilizationPct)
	}
}

// TestTelemetryCacheCarriesRoundTripUnaged: the round trip is a measurement,
// not an age, so the cache passes it through while MSSince advances.
func TestTelemetryCacheCarriesRoundTripUnaged(t *testing.T) {
	cache := newTelemetryCache()
	receivedAt := time.Unix(1_700_000_000, 0)
	input := noderec.NodeTelemetry{
		HostUUID:       "node-a",
		TelemetryValid: true,
		MSSince:        100,
		RoundTripMs:    23,
	}
	if _, ok := cache.Upsert(sourceScanner, input, receivedAt); !ok {
		t.Fatal("upsert rejected")
	}
	snapshot := cache.Snapshot(receivedAt.Add(5 * time.Second))
	if len(snapshot) != 1 || snapshot[0].MSSince != 5_100 || snapshot[0].RoundTripMs != 23 {
		t.Fatalf("snapshot = %+v, want msSince aged to 5100 and roundTripMs still 23", snapshot)
	}
}

// TestIngestTelemetryRelaysToSubscribedPeer: telemetry the broker ingests is
// pushed to the peer as discovery:node-telemetry, round trip included, once
// the peer has subscribed to discovery — and not before.
func TestIngestTelemetryRelaysToSubscribedPeer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	b := &Broker{codec: NewCodec(client), telemetry: newTelemetryCache()}
	value := noderec.NodeTelemetry{HostUUID: "node-a", TelemetryValid: true, MSSince: 10, RoundTripMs: 23}

	// Unsubscribed: nothing is written, so a read would block. Ingest with the
	// pipe unread; if the broker wrote, the write itself would block and this
	// test would hang, which is a failure a timeout catches.
	done := make(chan struct{})
	go func() {
		b.ingestTelemetryAt(sourceScanner, value, time.Now())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ingest blocked writing to an unsubscribed peer")
	}

	b.subMu.Lock()
	b.subscribed = true
	b.subMu.Unlock()
	go b.ingestTelemetryAt(sourceScanner, value, time.Now())
	msg, err := NewCodec(server).Read()
	if err != nil {
		t.Fatalf("read relayed notification: %v", err)
	}
	if msg.Method != noderec.NotifyNodeTelemetry {
		t.Fatalf("relayed method = %q, want %q", msg.Method, noderec.NotifyNodeTelemetry)
	}
	var got noderec.NodeTelemetry
	if err := json.Unmarshal(msg.Params, &got); err != nil {
		t.Fatalf("decode relayed telemetry: %v", err)
	}
	if got.HostUUID != "node-a" || got.RoundTripMs != 23 {
		t.Fatalf("relayed telemetry = %+v, want node-a with roundTripMs 23", got)
	}
}

func TestTelemetryCachePrefersScannerAndFallsBackToManual(t *testing.T) {
	cache := newTelemetryCache()
	now := time.Unix(1_700_000_000, 0)
	scanner := noderec.NodeTelemetry{
		HostUUID:          "node-a",
		GPUUtilizationPct: 20,
		TelemetryValid:    true,
	}
	manual := noderec.NodeTelemetry{
		HostUUID:          "node-a",
		GPUUtilizationPct: 70,
		TelemetryValid:    true,
	}

	cache.Upsert(sourceScanner, scanner, now)
	projected, ok := cache.Upsert(sourceManual, manual, now.Add(time.Second))
	if !ok || projected.GPUUtilizationPct != 20 {
		t.Fatalf("scanner projection = %+v, ok=%v", projected, ok)
	}

	projected, ok = cache.Remove("node-a", sourceScanner, now.Add(2*time.Second))
	if !ok || projected.GPUUtilizationPct != 70 || projected.MSSince != 1_000 {
		t.Fatalf("manual fallback = %+v, ok=%v", projected, ok)
	}

	projected, ok = cache.Remove("node-a", sourceManual, now.Add(3*time.Second))
	if !ok || projected.HostUUID != "node-a" || projected.TelemetryValid || projected.MSSince != 0 {
		t.Fatalf("final removal projection = %+v, ok=%v", projected, ok)
	}
	if snapshot := cache.Snapshot(now.Add(4 * time.Second)); len(snapshot) != 0 {
		t.Fatalf("cache retained final removal: %+v", snapshot)
	}
}

func TestTelemetryCacheSnapshotIsSortedAndNormalizesInvalidAge(t *testing.T) {
	cache := newTelemetryCache()
	now := time.Unix(1_700_000_000, 0)
	cache.Upsert(sourceScanner, noderec.NodeTelemetry{
		HostUUID:       "node-z",
		TelemetryValid: false,
		MSSince:        999,
	}, now)
	cache.Upsert(sourceScanner, noderec.NodeTelemetry{
		HostUUID:       "node-a",
		TelemetryValid: true,
		MSSince:        -50,
	}, now)

	got := cache.Snapshot(now)
	if len(got) != 2 || got[0].HostUUID != "node-a" || got[1].HostUUID != "node-z" {
		t.Fatalf("snapshot order = %+v", got)
	}
	if got[0].MSSince != 0 || got[1].MSSince != 0 {
		t.Fatalf("normalized ages = [%d %d], want [0 0]", got[0].MSSince, got[1].MSSince)
	}
}

func TestReplayTelemetryToSchedulerIncludesCurrentAge(t *testing.T) {
	brokerSide, schedulerSide := net.Pipe()
	t.Cleanup(func() {
		_ = brokerSide.Close()
		_ = schedulerSide.Close()
	})
	worker := &rpcWorker{peer: NewPeer(NewCodec(brokerSide))}
	b := &Broker{telemetry: newTelemetryCache()}
	b.telemetry.Upsert(sourceScanner, noderec.NodeTelemetry{
		HostUUID:          "node-a",
		GPUUtilizationPct: 45,
		TelemetryValid:    true,
		MSSince:           100,
	}, time.Now().Add(-250*time.Millisecond))

	replayed := make(chan int, 1)
	go func() { replayed <- b.replayTelemetryToScheduler(worker) }()
	if err := schedulerSide.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	message := readSchedulerTestMessage(t, NewCodec(schedulerSide))
	if message.Method != schedulerwire.MethodTelemetry {
		t.Fatalf("replay method = %q, want %q", message.Method, schedulerwire.MethodTelemetry)
	}
	var got noderec.NodeTelemetry
	if err := json.Unmarshal(message.Params, &got); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if got.HostUUID != "node-a" || got.MSSince < 350 {
		t.Fatalf("replayed telemetry = %+v, want aged node-a observation", got)
	}
	if count := <-replayed; count != 1 {
		t.Fatalf("replayed count = %d, want 1", count)
	}
}

func TestManualTelemetryReprojectsSurvivingAliasAtOriginalAge(t *testing.T) {
	b := newManualTestBroker()
	first := manualStatus("first", "10.0.0.1", "node-a")
	first.GPUs = []GPUInfo{{UtilizationPercent: 25}}
	first.TelemetryValid = true
	first.MSSince = 100
	b.upsertManualNode(first)

	b.manualMu.Lock()
	entry := b.manualNodeStatuses["first"]
	entry.receivedAt = time.Now().Add(-2 * time.Second)
	b.manualNodeStatuses["first"] = entry
	b.manualMu.Unlock()

	second := manualStatus("second", "10.0.0.2", "node-a")
	second.GPUs = []GPUInfo{{UtilizationPercent: 80}}
	second.TelemetryValid = true
	b.upsertManualNode(second)
	b.removeManualNode("second")

	snapshot := b.telemetry.Snapshot(time.Now())
	if len(snapshot) != 1 || snapshot[0].HostUUID != "node-a" {
		t.Fatalf("surviving manual telemetry = %+v", snapshot)
	}
	if snapshot[0].GPUUtilizationPct != 25 || snapshot[0].MSSince < 2_100 {
		t.Fatalf("surviving alias reset telemetry age: %+v", snapshot[0])
	}

	b.removeManualNode("first")
	if snapshot := b.telemetry.Snapshot(time.Now()); len(snapshot) != 0 {
		t.Fatalf("final manual alias left telemetry cached: %+v", snapshot)
	}
}
