// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import "sort"

// installConditions is the set of host conditions an Install.Extras entry
// may name in `when`. Validation rejects any other value at manifest load so
// a typo fails with a clear message rather than silently skipping a
// component. Each condition is evaluated on the installing host at install
// time by evalInstallCondition.
//
//   - "gpu:amd": an AMD GPU driven by the amdgpu kernel driver is present.
//     Detected from sysfs on Linux; false on every other OS, where the
//     bundled engines already ship their AMD runtime inside the primary
//     archive and no manifest needs the condition.
var installConditions = map[string]bool{
	"gpu:amd": true,
}

// installConditionList returns the condition names, sorted, so error
// messages cannot drift from the actual allow-set.
func installConditionList() []string {
	out := make([]string, 0, len(installConditions))
	for k := range installConditions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// evalInstallCondition reports whether a named condition holds on this host.
// Unknown names are false; Validate keeps them out of loaded manifests.
func evalInstallCondition(name string) bool {
	switch name {
	case "gpu:amd":
		return hostHasAMDGPU()
	default:
		return false
	}
}
