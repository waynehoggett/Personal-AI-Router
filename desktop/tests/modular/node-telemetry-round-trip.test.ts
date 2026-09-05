// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ emitBridgePush: vi.fn() }))
vi.mock('electron', () => ({ BrowserWindow: { getAllWindows: () => [] } }))
vi.mock('@/electron/window', () => ({ createOverviewWindow: vi.fn() }))
vi.mock('@/electron/service-bridge/broadcaster', () => ({ emitBridgePush: mocks.emitBridgePush }))

import { getModularBridgeState } from '@/electron/service-bridge/modular-state'

// The broker relays the scanner's per-node telemetry as discovery:node-telemetry.
// The bridge keeps only the round trip from it (hardware comes from its own
// node-info poll) and pushes a metrics update so the node card can show how far
// away the node is.
describe('node telemetry round trip', () => {
    beforeEach(() => {
        mocks.emitBridgePush.mockClear()
    })

    it('carries the relayed round trip into metrics and ignores repeats', () => {
        const state = getModularBridgeState()
        state.handleNotification({
            source: 'broker',
            method: 'discovery:nodes-changed',
            params: {
                nodes: [
                    {
                        hostUuid: 'uuid-rtt-1',
                        name: 'far-host',
                        ipAddress: '100.101.102.103',
                        port: 14318
                    }
                ]
            }
        })
        mocks.emitBridgePush.mockClear()

        state.handleNotification({
            source: 'broker',
            method: 'discovery:node-telemetry',
            params: {
                hostUuid: 'uuid-rtt-1',
                gpuUtilizationPercent: 40,
                telemetryValid: true,
                msSince: 12,
                roundTripMs: 23
            }
        })
        const pushes = mocks.emitBridgePush.mock.calls.filter(call => call[0] === 'metrics:update')
        expect(pushes).toHaveLength(1)
        expect(pushes[0][1]).toMatchObject({ id: 'uuid-rtt-1', current: { roundTripMs: 23 } })

        // The same figure again is not news.
        mocks.emitBridgePush.mockClear()
        state.handleNotification({
            source: 'broker',
            method: 'discovery:node-telemetry',
            params: { hostUuid: 'uuid-rtt-1', roundTripMs: 23 }
        })
        expect(mocks.emitBridgePush).not.toHaveBeenCalledWith('metrics:update', expect.anything())

        // A node the bridge does not know is ignored rather than created.
        state.handleNotification({
            source: 'broker',
            method: 'discovery:node-telemetry',
            params: { hostUuid: 'uuid-unknown', roundTripMs: 5 }
        })
        expect(state.getNodesInitial().nodes['uuid-unknown']).toBeUndefined()
    })
})
