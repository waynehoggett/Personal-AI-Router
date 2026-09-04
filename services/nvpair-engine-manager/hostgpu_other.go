// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package main

// hostHasAMDGPU is false off Linux. Windows engine archives already carry
// their AMD runtime and macOS has no ROCm, so no bundled manifest gates an
// extra on this condition there; see installConditions.
func hostHasAMDGPU() bool { return false }
