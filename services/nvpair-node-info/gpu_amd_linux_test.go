// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeDRMCard adds one DRM card to a fake /sys/class/drm tree. The card's
// device entry is a symlink into a devices/ subtree, as it is on a real
// host, so the PCI-address join key is exercised rather than mocked. attrs
// are written as files under the device directory; a nil attrs still
// creates the device directory.
func fakeDRMCard(t *testing.T, root, card, pciAddr string, attrs map[string]string) {
	t.Helper()
	devDir := filepath.Join(root, "devices", pciAddr)
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, val := range attrs {
		if err := os.WriteFile(filepath.Join(devDir, name), []byte(val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cardDir := filepath.Join(root, card)
	if err := os.MkdirAll(cardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "devices", pciAddr), filepath.Join(cardDir, "device")); err != nil {
		t.Fatal(err)
	}
}

// fakeAMDTree builds the tree every test below shares: one amdgpu card with a
// board-supplied product name, one without, a connector node, an NVIDIA card,
// and an AMD card on the legacy radeon driver (no amdgpu memory attributes).
func fakeAMDTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fakeDRMCard(t, root, "card0", "0000:03:00.0", map[string]string{
		"vendor":              "0x1002",
		"product_name":        "AMD Radeon RX 7900 XTX",
		"mem_info_vram_total": "25753026560",
		"mem_info_vram_used":  "1073741824",
		"gpu_busy_percent":    "37",
	})
	fakeDRMCard(t, root, "card1", "0000:0c:00.0", map[string]string{
		"vendor":              "0x1002",
		"mem_info_vram_total": "536870912",
		"mem_info_vram_used":  "268435456",
		"gpu_busy_percent":    "150", // clamped
	})
	// Connector child of card0: same name prefix, must not be enumerated.
	if err := os.MkdirAll(filepath.Join(root, "card0-DP-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDRMCard(t, root, "card2", "0000:01:00.0", map[string]string{
		"vendor":              "0x10de",
		"mem_info_vram_total": "0",
	})
	fakeDRMCard(t, root, "card3", "0000:05:00.0", map[string]string{
		"vendor": "0x1002", // legacy radeon: no mem_info_* attributes
	})
	return root
}

// TestScanAMDGPUs pins enumeration: only amdgpu-driven AMD cards, keyed by
// the PCI address behind the device symlink, in stable order, with the
// board product name carried when present.
func TestScanAMDGPUs(t *testing.T) {
	root := fakeAMDTree(t)
	got := scanAMDGPUs(root)
	want := []amdgpuDevice{
		{key: "0000:03:00.0", dir: filepath.Join(root, "card0", "device"), name: "AMD Radeon RX 7900 XTX"},
		{key: "0000:0c:00.0", dir: filepath.Join(root, "card1", "device"), name: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanAMDGPUs() = %+v, want %+v", got, want)
	}
}

// TestScanAMDGPUsMissingRoot pins the no-DRM-class case (a container, or a
// headless VM): nil, not an error or a panic.
func TestScanAMDGPUsMissingRoot(t *testing.T) {
	if got := scanAMDGPUs(filepath.Join(t.TempDir(), "absent")); got != nil {
		t.Fatalf("scanAMDGPUs(missing) = %+v, want nil", got)
	}
}

// TestAMDGPUStatic pins the static records: total VRAM in bytes, the PCI
// address as statsKey, and the three-step name resolution — sysfs
// product_name, then the PCI database by address, then the generic label.
func TestAMDGPUStatic(t *testing.T) {
	root := fakeAMDTree(t)
	devs := scanAMDGPUs(root)

	t.Run("pci database fills a missing board name", func(t *testing.T) {
		names := map[string]string{
			"0000:0c:00.0": "Raphael",
			"0000:03:00.0": "Navi 31 [Radeon RX 7900 XT/7900 XTX]", // loses to product_name
		}
		got := amdgpuStatic(devs, names)
		want := []GPUInfo{
			{Name: "AMD Radeon RX 7900 XTX", VramBytes: 25753026560, statsKey: "0000:03:00.0"},
			{Name: "Raphael", VramBytes: 536870912, statsKey: "0000:0c:00.0"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("amdgpuStatic() = %+v, want %+v", got, want)
		}
	})

	t.Run("generic label when nothing names the card", func(t *testing.T) {
		got := amdgpuStatic(devs, nil)
		if got[1].Name != amdgpuDefaultName {
			t.Fatalf("unnamed card = %q, want %q", got[1].Name, amdgpuDefaultName)
		}
	})
}

// TestAMDGPUDynamic pins the per-tick read: busy percent clamped to 100 and
// VRAM-used in bytes keyed by statsKey, with the sample count reflecting
// utilization readings only.
func TestAMDGPUDynamic(t *testing.T) {
	root := fakeAMDTree(t)
	devs := scanAMDGPUs(root)

	out := map[string]gpuStat{}
	samples := amdgpuDynamic(devs, out)
	want := map[string]gpuStat{
		"0000:03:00.0": {UtilizationPct: 37, VRAMUsed: 1073741824},
		"0000:0c:00.0": {UtilizationPct: 100, VRAMUsed: 268435456},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("amdgpuDynamic() = %+v, want %+v", out, want)
	}
	if samples != 2 {
		t.Fatalf("samples = %d, want 2", samples)
	}

	// A card whose busy counter is missing still reports memory but must not
	// count as a utilization sample, mirroring the nvidia-smi [N/A] rule.
	if err := os.Remove(filepath.Join(devs[0].dir, "gpu_busy_percent")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devs[1].dir, "gpu_busy_percent"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = map[string]gpuStat{}
	samples = amdgpuDynamic(devs, out)
	want = map[string]gpuStat{
		"0000:03:00.0": {VRAMUsed: 1073741824},
		"0000:0c:00.0": {VRAMUsed: 268435456},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("amdgpuDynamic() without busy = %+v, want %+v", out, want)
	}
	if samples != 0 {
		t.Fatalf("samples without busy = %d, want 0", samples)
	}
}

// TestAMDGPUDynamicNoDevices pins that an empty device list touches nothing:
// the NVIDIA-only and GPU-less paths must be unaffected by the AMD reader.
func TestAMDGPUDynamicNoDevices(t *testing.T) {
	out := map[string]gpuStat{}
	if samples := amdgpuDynamic(nil, out); samples != 0 || len(out) != 0 {
		t.Fatalf("amdgpuDynamic(nil) = %d samples, %d entries; want 0, 0", samples, len(out))
	}
}

// TestReadSysfsUint pins the attribute parser's rejection of anything that is
// not a plain non-negative decimal, since sysfs never pads or signs these.
func TestReadSysfsUint(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name   string
		body   string
		want   uint64
		wantOK bool
	}{
		{"decimal", "4096\n", 4096, true},
		{"zero", "0\n", 0, true},
		{"negative", "-1\n", 0, false},
		{"text", "N/A\n", 0, false},
		{"empty", "", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(dir, c.name)
			if err := os.WriteFile(p, []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, ok := readSysfsUint(p)
			if ok != c.wantOK || got != c.want {
				t.Fatalf("readSysfsUint(%q) = %d, %v; want %d, %v", c.body, got, ok, c.want, c.wantOK)
			}
		})
	}
	if _, ok := readSysfsUint(filepath.Join(dir, "missing")); ok {
		t.Fatal("missing attribute reported ok")
	}
}
