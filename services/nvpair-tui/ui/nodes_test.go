// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui

import "testing"

// TestNodesViewRoundTripColumn pins the RTT column: a relayed round trip lands
// in the row for that node, a repeat of the same figure is not a redraw, and an
// unmeasured node shows a dash.
func TestNodesViewRoundTripColumn(t *testing.T) {
	v := newNodesView(nil)
	v.SetSize(120, 20)
	v.setNodes([]availableNode{
		{HostUUID: "near", Name: "near", IPAddress: "10.0.0.2", Port: 14318},
		{HostUUID: "far", Name: "far", IPAddress: "100.101.102.103", Port: 14318},
	})
	if got := v.table.Rows()[1][4]; got != "-" {
		t.Fatalf("unmeasured RTT cell = %q, want -", got)
	}

	if !v.applyTelemetry(nodeTelemetry{HostUUID: "far", RoundTripMs: 23}) {
		t.Fatal("first round trip should redraw")
	}
	v.setNodes(v.nodes)
	if got := v.table.Rows()[1][4]; got != "23 ms" {
		t.Fatalf("RTT cell = %q, want 23 ms", got)
	}
	if got := v.table.Rows()[0][4]; got != "-" {
		t.Fatalf("other node's RTT cell = %q, want -", got)
	}
	if v.applyTelemetry(nodeTelemetry{HostUUID: "far", RoundTripMs: 23}) {
		t.Fatal("unchanged round trip should not redraw")
	}
	if v.applyTelemetry(nodeTelemetry{HostUUID: "", RoundTripMs: 9}) || v.applyTelemetry(nodeTelemetry{HostUUID: "far"}) {
		t.Fatal("malformed telemetry must be ignored")
	}
}
