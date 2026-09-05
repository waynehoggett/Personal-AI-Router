// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

export interface GpuMetricValue {
    id: string // GPU ID from topology
    value: number // percentage
}

export interface NodeItemMetricsEntry {
    timestamp: number // milliseconds since epoch
    cpuUtilization: number // percentage
    memoryUsage: number // percentage
    gpuUtilization: GpuMetricValue[] // percentage per GPU
    gpuVramUsage: GpuMetricValue[] // percentage per GPU
    // Smoothed network round trip from this machine's node scanner to the node,
    // in whole milliseconds. 0 until a sample exists. Relayed by the broker from
    // the same telemetry the scheduler sees, so it is one figure everywhere.
    roundTripMs: number
}

export interface NodeItemMetrics {
    id: string
    current: NodeItemMetricsEntry
    historical: NodeItemMetricsEntry[]
}
