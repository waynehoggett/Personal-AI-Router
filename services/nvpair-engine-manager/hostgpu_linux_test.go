// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeDRMCard writes one cardN/device tree under root with the given sysfs
// attributes. A real host uses a symlink into the PCI tree; the condition
// only reads through the path, so a plain directory is equivalent here.
func fakeDRMCard(t *testing.T, root, card string, attrs map[string]string) {
	t.Helper()
	dev := filepath.Join(root, card, "device")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, val := range attrs {
		if err := os.WriteFile(filepath.Join(dev, name), []byte(val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHostHasAMDGPU pins the gpu:amd condition: true only for an AMD card
// bound to amdgpu (vendor 0x1002 plus the driver's VRAM attribute), never
// for other vendors, a radeon-bound AMD card, connector nodes, or a host
// with no DRM class at all.
func TestHostHasAMDGPU(t *testing.T) {
	t.Run("amdgpu card", func(t *testing.T) {
		root := t.TempDir()
		fakeDRMCard(t, root, "card0", map[string]string{"vendor": "0x10de", "mem_info_vram_total": "0"})
		fakeDRMCard(t, root, "card1", map[string]string{"vendor": "0x1002", "mem_info_vram_total": "17163091968"})
		if !hostHasAMDGPUIn(root) {
			t.Fatal("amdgpu card not detected")
		}
	})
	t.Run("nvidia only", func(t *testing.T) {
		root := t.TempDir()
		fakeDRMCard(t, root, "card0", map[string]string{"vendor": "0x10de"})
		if hostHasAMDGPUIn(root) {
			t.Fatal("NVIDIA-only host reported an AMD GPU")
		}
	})
	t.Run("legacy radeon driver", func(t *testing.T) {
		root := t.TempDir()
		fakeDRMCard(t, root, "card0", map[string]string{"vendor": "0x1002"})
		if hostHasAMDGPUIn(root) {
			t.Fatal("radeon-bound card must not satisfy gpu:amd")
		}
	})
	t.Run("connector node ignored", func(t *testing.T) {
		root := t.TempDir()
		fakeDRMCard(t, root, "card0-DP-1", map[string]string{"vendor": "0x1002", "mem_info_vram_total": "1"})
		if hostHasAMDGPUIn(root) {
			t.Fatal("connector node must not be treated as an adapter")
		}
	})
	t.Run("missing root", func(t *testing.T) {
		if hostHasAMDGPUIn(filepath.Join(t.TempDir(), "absent")) {
			t.Fatal("missing DRM class reported an AMD GPU")
		}
	})
}
