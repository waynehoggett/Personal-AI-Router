// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useState } from 'react'
import {
    Button,
    Checkbox,
    Divider,
    Flex,
    FormField,
    ModalContent,
    ModalDialog,
    ModalRoot,
    Stack,
    Text,
    TextInput
} from '@nvidia/foundations-react-core'
import getErrorString from '@/shared/utils/get-error-string'
import { DialogHeader } from './DialogHeader'
import { InlineErrorBanner } from './InlineErrorBanner'
import { InvitePairingPanel } from './InvitePairingPanel'
import { useBlurOnOpen } from '@/ui/hooks/useBlurOnOpen'
import { useInvitePairing } from '@/ui/hooks/useInvitePairing'
import { useInvitablePeers } from '@/ui/hooks/useInvitablePeers'

interface AddNodeModalProps {
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function AddNodeModal({ open, onOpenChange }: AddNodeModalProps) {
    useBlurOnOpen(open)
    const [manualIp, setManualIp] = useState('')
    // A node on another network (a Tailscale peer, a routed subnet) is never
    // seen by discovery, so the invite alone would leave it out of the node
    // directory: no telemetry, no model list, nothing to route to. Registering
    // it as a manual node first makes the prober fold it in like a discovered
    // one. Off by default: a LAN node discovery already sees needs no entry.
    const [remoteNode, setRemoteNode] = useState(false)
    const [remoteNodeError, setRemoteNodeError] = useState<string | null>(null)
    const pairing = useInvitePairing()
    const nodesThatCanBeAdded = useInvitablePeers()

    const handleOpenChange = useCallback(
        (next: boolean) => {
            setManualIp('')
            setRemoteNode(false)
            setRemoteNodeError(null)
            pairing.reset()
            onOpenChange(next)
        },
        [onOpenChange, pairing]
    )

    const handleManualInvite = useCallback(async () => {
        const ip = manualIp.trim()
        if (!ip) return
        setRemoteNodeError(null)
        if (remoteNode) {
            try {
                await window.pairApi.nodes.addManual(ip)
            } catch (err) {
                setRemoteNodeError(`Could not register ${ip}: ${getErrorString(err)}`)
                return
            }
        }
        await pairing.start(ip)
    }, [manualIp, pairing, remoteNode])

    const showPairing = pairing.invite !== null || pairing.error !== null
    const inviteInFlight = pairing.submitting || pairing.invite?.state === 'pending'

    return (
        <ModalRoot open={open} onOpenChange={handleOpenChange} hideCloseButton>
            <ModalDialog>
                <ModalContent className="no-drag-elements max-content-modal">
                    <DialogHeader onClose={() => handleOpenChange(false)}>
                        <Flex align="center" gap="2">
                            <span>Add node</span>
                            {pairing.submitting && (
                                <span className="spinner-element" role="status" aria-label="" />
                            )}
                        </Flex>
                    </DialogHeader>
                    <Stack gap="4" className="pt-2">
                        {showPairing ? (
                            <InvitePairingPanel
                                invite={pairing.invite}
                                error={pairing.error}
                                onReset={pairing.reset}
                                onCancel={() => void pairing.cancel()}
                                onDone={() => handleOpenChange(false)}
                            />
                        ) : (
                            <>
                                <Flex align="end" gap="2">
                                    <FormField slotLabel="IP address" className="flex-1">
                                        <TextInput
                                            value={manualIp}
                                            onValueChange={setManualIp}
                                            placeholder="192.168.1.100"
                                            onKeyDown={event => {
                                                if (event.key === 'Enter') void handleManualInvite()
                                            }}
                                            disabled={inviteInFlight}
                                        />
                                    </FormField>
                                    <Button
                                        kind="primary"
                                        color="brand"
                                        onClick={() => void handleManualInvite()}
                                        disabled={!manualIp.trim() || inviteInFlight}
                                    >
                                        Invite
                                    </Button>
                                </Flex>
                                <Flex align="start" gap="2">
                                    <Checkbox
                                        checked={remoteNode}
                                        onCheckedChange={checked => setRemoteNode(checked === true)}
                                        disabled={inviteInFlight}
                                        aria-label="Node is on another network"
                                        className="mt-0.5"
                                    />
                                    <Stack gap="0">
                                        <Text kind="body/regular/sm">
                                            Node is on another network
                                        </Text>
                                        <Text kind="body/regular/sm" className="text-subtle-color">
                                            For a node reached over Tailscale or a routed subnet,
                                            which discovery cannot see. PAIR keeps the address and
                                            polls it directly. Use the node&apos;s Tailscale IP.
                                        </Text>
                                    </Stack>
                                </Flex>
                                {remoteNodeError && (
                                    <InlineErrorBanner
                                        message={remoteNodeError}
                                        onClose={() => setRemoteNodeError(null)}
                                    />
                                )}

                                {nodesThatCanBeAdded.length > 0 && (
                                    <Stack gap="4" className="mt-1">
                                        <Divider />

                                        <Stack gap="1">
                                            {nodesThatCanBeAdded.map(node => (
                                                <Flex
                                                    key={node.id}
                                                    align="center"
                                                    justify="between"
                                                    gap="2"
                                                    className="py-1"
                                                >
                                                    <Stack gap="0">
                                                        <Text
                                                            kind="body/semibold/sm"
                                                            className="uppercase"
                                                        >
                                                            {node.name || node.ipAddress}
                                                        </Text>
                                                        <Text
                                                            kind="body/regular/sm"
                                                            className="text-subtle-color"
                                                        >
                                                            {node.clustered
                                                                ? 'In another cluster'
                                                                : `${node.ipAddress}:${node.port}`}
                                                        </Text>
                                                    </Stack>
                                                    <Button
                                                        kind="primary"
                                                        color="brand"
                                                        size="small"
                                                        onClick={() =>
                                                            void pairing.start(node.ipAddress)
                                                        }
                                                        // A node already in a cluster cannot join
                                                        // another; the backend would reject the
                                                        // invite (`rejected` / `already-clustered`).
                                                        disabled={inviteInFlight || node.clustered}
                                                    >
                                                        Invite
                                                    </Button>
                                                </Flex>
                                            ))}
                                        </Stack>
                                    </Stack>
                                )}
                            </>
                        )}
                    </Stack>
                </ModalContent>
            </ModalDialog>
        </ModalRoot>
    )
}
