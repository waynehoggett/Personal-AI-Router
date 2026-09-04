// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jaypipes/ghw"
)

// AMD GPU support on Linux reads the amdgpu kernel driver's sysfs attributes
// directly instead of shelling out to a vendor tool. rocm-smi needs the ROCm
// stack installed, while these files exist on every host whose GPU is driven
// by amdgpu — which is every AMD adapter from GCN onwards on any current
// kernel — so a plain desktop with the in-tree driver reports VRAM and
// utilization without installing anything.
//
// Per card (/sys/class/drm/cardN/device/):
//
//   - vendor               : PCI vendor id, "0x1002" for AMD.
//   - mem_info_vram_total  : dedicated VRAM in bytes (static).
//   - mem_info_vram_used   : dedicated VRAM in use, bytes (dynamic).
//   - gpu_busy_percent     : GPU busy percentage over the last sampling
//     window (dynamic).
//   - product_name         : marketing name on boards that expose one; most
//     consumer cards leave it empty, so the PCI database via ghw fills in.
//
// mem_info_vram_total exists only under amdgpu; a card still bound to the
// legacy radeon driver has no such file and is left to the ghw name-only
// fallback like any other adapter without a dynamic source.
//
// On an APU the "VRAM" figures describe the BIOS carve-out, not the shared
// system memory the GPU can also map through GTT, so an integrated AMD GPU
// reads understated — the same caveat Windows carries for integrated
// adapters.

// amdgpuSysfsRoot is the DRM class directory the collector enumerates.
const amdgpuSysfsRoot = "/sys/class/drm"

// amdPCIVendorID is the PCI vendor id sysfs reports for AMD/ATI adapters.
const amdPCIVendorID = "0x1002"

// amdgpuDefaultName is used when neither sysfs nor the PCI database names
// the adapter.
const amdgpuDefaultName = "AMD Radeon Graphics"

// drmCardRe matches the per-adapter DRM nodes (card0, card1, ...) and not
// their connector children (card0-DP-1, card0-HDMI-A-1, ...), which are
// siblings under the same class directory.
var drmCardRe = regexp.MustCompile(`^card[0-9]+$`)

// amdgpuDevice is one amdgpu-driven adapter found under sysfs. key is the PCI
// address the card's device symlink resolves to (e.g. "0000:03:00.0"): it is
// stable across reboots on a fixed machine, unique per host, and is what
// static detection stamps as statsKey so the dynamic collector can join back
// to it. dir is the device directory the dynamic attributes are read from.
type amdgpuDevice struct {
	key  string
	dir  string
	name string // product_name from sysfs when the board provides one
}

// scanAMDGPUs enumerates amdgpu-driven adapters under root. Cards of other
// vendors, connector nodes, and AMD cards without amdgpu's memory attributes
// are skipped. The result is sorted by card name so output order is stable
// across ticks. A missing or unreadable root yields nil.
func scanAMDGPUs(root string) []amdgpuDevice {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var devs []amdgpuDevice
	for _, e := range entries {
		if !drmCardRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(root, e.Name(), "device")
		vendor, ok := readSysfsString(filepath.Join(dir, "vendor"))
		if !ok || !strings.EqualFold(vendor, amdPCIVendorID) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "mem_info_vram_total")); err != nil {
			continue
		}
		name, _ := readSysfsString(filepath.Join(dir, "product_name"))
		devs = append(devs, amdgpuDevice{
			key:  amdgpuKey(dir, e.Name()),
			dir:  dir,
			name: name,
		})
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].dir < devs[j].dir })
	return devs
}

// amdgpuKey derives the join key for a card: the PCI address its device
// symlink points at. When the link cannot be resolved the DRM node name is
// used instead, which is still unique on the host.
func amdgpuKey(deviceDir, cardName string) string {
	resolved, err := filepath.EvalSymlinks(deviceDir)
	if err != nil {
		return "amdgpu:" + cardName
	}
	return strings.ToLower(filepath.Base(resolved))
}

// readSysfsString reads a single-value sysfs attribute, trimmed. ok is false
// when the file is missing or unreadable.
func readSysfsString(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// readSysfsUint reads a decimal sysfs attribute. ok is false when the file is
// missing, unreadable, or not a non-negative integer.
func readSysfsUint(path string) (uint64, bool) {
	s, ok := readSysfsString(path)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// detectAMDGPUs is the static enumeration used by detectGPUs: every amdgpu
// adapter with its name, total VRAM, and statsKey. Names come from sysfs
// product_name first, then the PCI database via ghw keyed by address, then a
// generic label.
func detectAMDGPUs() []GPUInfo {
	devs := scanAMDGPUs(amdgpuSysfsRoot)
	if len(devs) == 0 {
		return nil
	}
	return amdgpuStatic(devs, ghwNamesByAddress())
}

// amdgpuStatic builds GPUInfo records for the scanned devices. names maps a
// lowercase PCI address to the PCI-database product name and may be nil.
func amdgpuStatic(devs []amdgpuDevice, names map[string]string) []GPUInfo {
	gpus := make([]GPUInfo, 0, len(devs))
	for _, d := range devs {
		name := d.name
		if name == "" {
			name = names[d.key]
		}
		if name == "" {
			name = amdgpuDefaultName
		}
		total, _ := readSysfsUint(filepath.Join(d.dir, "mem_info_vram_total"))
		gpus = append(gpus, GPUInfo{
			Name:      name,
			VramBytes: total,
			statsKey:  d.key,
		})
	}
	return gpus
}

// amdgpuDynamic reads the per-tick attributes for each device into out, keyed
// by statsKey, and returns how many devices produced a utilization reading.
// A device whose busy attribute is missing or malformed still contributes
// its VRAM-used figure, but does not count as a utilization sample so the
// node-wide telemetry validity is not claimed on memory-only data — the same
// rule parseNvidiaDynamic applies to [N/A] rows.
func amdgpuDynamic(devs []amdgpuDevice, out map[string]gpuStat) int {
	samples := 0
	for _, d := range devs {
		var stat gpuStat
		if pct, ok := readSysfsUint(filepath.Join(d.dir, "gpu_busy_percent")); ok {
			if pct > 100 {
				pct = 100
			}
			stat.UtilizationPct = uint32(pct)
			samples++
		}
		if used, ok := readSysfsUint(filepath.Join(d.dir, "mem_info_vram_used")); ok {
			stat.VRAMUsed = used
		}
		out[d.key] = stat
	}
	return samples
}

// ghwNamesByAddress returns the PCI-database product name of every graphics
// card ghw can see, keyed by lowercase PCI address. A ghw failure is logged
// and yields an empty map so callers fall through to the generic name.
func ghwNamesByAddress() map[string]string {
	names := map[string]string{}
	gpu, err := ghw.GPU()
	if err != nil {
		slog.Debug("ghw GPU lookup failed; AMD adapters keep generic names", "err", err)
		return names
	}
	for _, card := range gpu.GraphicsCards {
		if card.DeviceInfo == nil || card.DeviceInfo.Product == nil || card.Address == "" {
			continue
		}
		names[strings.ToLower(card.Address)] = card.DeviceInfo.Product.Name
	}
	return names
}
