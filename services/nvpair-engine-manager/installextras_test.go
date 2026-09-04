// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestInstallExtrasGatedOnHostCondition drives an install whose manifest
// declares a conditional extra. When the host condition holds the extra is
// downloaded and its run command executes after the primary one; when it
// does not, nothing is fetched and the install still succeeds.
func TestInstallExtrasGatedOnHostCondition(t *testing.T) {
	for _, tc := range []struct {
		name      string
		condition bool
	}{
		{"condition met installs the extra", true},
		{"condition unmet skips the extra", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var primary, extra atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/primary":
					primary.Add(1)
				case "/extra":
					extra.Add(1)
				default:
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte("archive"))
			}))
			defer srv.Close()

			baseDir := t.TempDir()
			installDir := filepath.Join(baseDir, "fake")
			m := &Manifest{
				Engine: "fake", DisplayName: "Fake", ManifestVersion: 1,
				Platforms: map[string]Platform{hostKey(): {
					Detect: []string{filepath.Join(installDir, "main")},
					Install: &Install{
						Fetch: &Fetch{URL: srv.URL + "/primary"},
						Run:   []string{fakeEngineBin, "touch", "{install_dir}/main"},
						Extras: []InstallExtra{{
							When:  "gpu:amd",
							Fetch: &Fetch{URL: srv.URL + "/extra"},
							Run:   []string{fakeEngineBin, "touch", "{install_dir}/extra"},
						}},
					},
					Runtime: Runtime{Bin: fakeEngineBin},
				}},
			}
			if err := m.Validate(); err != nil {
				t.Fatalf("manifest should validate: %v", err)
			}
			reg := NewRegistry()
			reg.engines[m.Engine] = m
			ex := NewExecutor(reg, NewReporter(nil), func(string, any) {}, baseDir)
			ex.detectTimeout = 2 * time.Second
			var asked []string
			ex.installCondition = func(name string) bool {
				asked = append(asked, name)
				return tc.condition
			}

			if err := ex.Install(context.Background(), "fake"); err != nil {
				t.Fatalf("install: %v", err)
			}
			if len(asked) != 1 || asked[0] != "gpu:amd" {
				t.Fatalf("conditions evaluated = %v, want [gpu:amd]", asked)
			}
			if got := primary.Load(); got != 1 {
				t.Fatalf("primary downloads = %d, want 1", got)
			}
			if _, err := os.Stat(filepath.Join(installDir, "main")); err != nil {
				t.Fatalf("primary run did not execute: %v", err)
			}
			wantExtra := int32(0)
			if tc.condition {
				wantExtra = 1
			}
			if got := extra.Load(); got != wantExtra {
				t.Fatalf("extra downloads = %d, want %d", got, wantExtra)
			}
			_, err := os.Stat(filepath.Join(installDir, "extra"))
			if tc.condition && err != nil {
				t.Fatalf("extra run did not execute: %v", err)
			}
			if !tc.condition && err == nil {
				t.Fatal("extra run executed although its condition was unmet")
			}
		})
	}
}

// TestInstallExtraFailureFailsInstall pins that a wanted extra is not
// best-effort: a checksum mismatch on it fails the install and is reported,
// so a host never ends up with a silently CPU-only engine it believes has
// its GPU runtime.
func TestInstallExtraFailureFailsInstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("archive"))
	}))
	defer srv.Close()
	baseDir := t.TempDir()
	m := &Manifest{
		Engine: "fake", DisplayName: "Fake", ManifestVersion: 1,
		Platforms: map[string]Platform{hostKey(): {
			Detect: []string{filepath.Join(baseDir, "fake", "main")},
			Install: &Install{
				Fetch: &Fetch{URL: srv.URL + "/primary"},
				Run:   []string{fakeEngineBin, "touch", "{install_dir}/main"},
				Extras: []InstallExtra{{
					When:  "gpu:amd",
					Fetch: &Fetch{URL: srv.URL + "/extra", SHA256: "deadbeef"},
				}},
			},
			Runtime: Runtime{Bin: fakeEngineBin},
		}},
	}
	reg := NewRegistry()
	reg.engines[m.Engine] = m
	ex := NewExecutor(reg, NewReporter(nil), func(string, any) {}, baseDir)
	ex.detectTimeout = 2 * time.Second
	ex.installCondition = func(string) bool { return true }

	err := ex.Install(context.Background(), "fake")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") || !strings.Contains(err.Error(), "gpu:amd") {
		t.Fatalf("install error = %v, want a checksum mismatch naming the extra", err)
	}
	if !hasErr(ex.Errors(), installFailedID("fake")) {
		t.Fatalf("expected install-failed to be reported, got %+v", ex.Errors())
	}
}

// TestInstallExtrasValidation pins the load-time checks: a known condition,
// a fetch URL, no coexistence with a script install, and placeholder
// scanning of the extra's run command.
func TestInstallExtrasValidation(t *testing.T) {
	base := func() *Manifest {
		return &Manifest{
			Engine: "x", DisplayName: "X", ManifestVersion: 1,
			Platforms: map[string]Platform{"linux/amd64": {
				Install: &Install{
					Fetch:  &Fetch{URL: "https://example/x.tgz"},
					Run:    []string{"tar", "-xf", "{download}"},
					Extras: []InstallExtra{{When: "gpu:amd", Fetch: &Fetch{URL: "https://example/x-rocm.tgz"}, Run: []string{"tar", "-xf", "{download}"}}},
				},
				Runtime: Runtime{Bin: "{install_dir}/x"},
			}},
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("well-formed extras rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(p *Platform)
		want   string
	}{
		{"unknown condition", func(p *Platform) { p.Install.Extras[0].When = "gpu:intel" }, "when \"gpu:intel\" unknown"},
		{"missing fetch", func(p *Platform) { p.Install.Extras[0].Fetch = nil }, "extras[0].fetch.url is required"},
		{"empty fetch url", func(p *Platform) { p.Install.Extras[0].Fetch.URL = " " }, "extras[0].fetch.url is required"},
		{"script install", func(p *Platform) {
			p.Install.Fetch, p.Install.Run = nil, nil
			p.Install.Script = []string{"sh", "install.sh"}
		}, "cannot accompany a script install"},
		{"bad placeholder", func(p *Platform) { p.Install.Extras[0].Run = []string{"tar", "{bogus}"} }, "unknown placeholder {bogus}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base()
			p := m.Platforms["linux/amd64"]
			c.mutate(&p)
			m.Platforms["linux/amd64"] = p
			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, c.want)
			}
		})
	}
}

// TestBundledOllamaLinuxROCmExtra pins the shipped manifest: the Linux x64
// Ollama install carries the ROCm runtime as a gpu:amd extra unpacked into
// the same install dir, and no other platform declares one (Windows archives
// already bundle it; macOS and arm64 have no ROCm build).
func TestBundledOllamaLinuxROCmExtra(t *testing.T) {
	reg := NewRegistry()
	if err := reg.LoadFS(bundledManifests, "manifests"); err != nil {
		t.Fatal(err)
	}
	ol, ok := reg.Get("ollama")
	if !ok {
		t.Fatal("bundled ollama manifest missing")
	}
	for key, p := range ol.Platforms {
		if p.Install == nil {
			continue
		}
		if key != "linux/amd64" {
			if len(p.Install.Extras) != 0 {
				t.Errorf("%s: unexpected extras %+v", key, p.Install.Extras)
			}
			continue
		}
		if len(p.Install.Extras) != 1 {
			t.Fatalf("linux/amd64 extras = %+v, want exactly the ROCm bundle", p.Install.Extras)
		}
		x := p.Install.Extras[0]
		if x.When != "gpu:amd" || !strings.Contains(x.Fetch.URL, "ollama-linux-amd64-rocm") {
			t.Fatalf("linux/amd64 extra = %+v, want gpu:amd ROCm fetch", x)
		}
		if strings.Join(x.Run, " ") != strings.Join(p.Install.Run, " ") {
			t.Fatalf("ROCm extra run %v must unpack like the primary %v", x.Run, p.Install.Run)
		}
	}
}
