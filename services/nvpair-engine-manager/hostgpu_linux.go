// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// drmClassDir is where the kernel lists DRM adapters, one cardN directory
// per GPU, each with a device symlink into the PCI tree.
const drmClassDir = "/sys/class/drm"

// amdPCIVendorID is the PCI vendor id sysfs reports for AMD/ATI adapters.
const amdPCIVendorID = "0x1002"

// drmCardRe matches per-adapter nodes (card0, card1, ...) and not their
// connector children (card0-DP-1, ...).
var drmCardRe = regexp.MustCompile(`^card[0-9]+$`)

// hostHasAMDGPU reports whether any DRM adapter is an AMD card bound to the
// amdgpu driver, which is the same test node-info's Linux collector applies
// before it reports AMD telemetry: PCI vendor 0x1002 plus amdgpu's
// mem_info_vram_total attribute, which the legacy radeon driver never
// exposes. ROCm supports only amdgpu-driven cards, so a radeon-bound card
// gains nothing from the ROCm bundle and is deliberately not matched.
func hostHasAMDGPU() bool {
	return hostHasAMDGPUIn(drmClassDir)
}

func hostHasAMDGPUIn(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !drmCardRe.MatchString(e.Name()) {
			continue
		}
		dev := filepath.Join(root, e.Name(), "device")
		vendor, err := os.ReadFile(filepath.Join(dev, "vendor"))
		if err != nil || !strings.EqualFold(strings.TrimSpace(string(vendor)), amdPCIVendorID) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dev, "mem_info_vram_total")); err == nil {
			return true
		}
	}
	return false
}
