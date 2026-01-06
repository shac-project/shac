// Copyright 2025 The Shac Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// FUSE super_magic value. This value is returned by the stat() syscall on
// FUSE filesystems.
const fuseSuperMagic = 0x65735546

// IsFuse checks if the given path is on a FUSE filesystem.
func isFuse(path string) bool {
	var buf syscall.Statfs_t
	if err := syscall.Statfs(path, &buf); err != nil {
		return false
	}
	return buf.Type == fuseSuperMagic
}

// resolveFuseMounts checks if the given mounts traverse FUSE-managed symlinks.
// If so, it returns a new list of mounts where such mounts are replaced by
// mounts of their resolved target paths, binding them to the original destinations.
func resolveFuseMounts(root, exe string, mounts []Mount) []Mount {
	return resolveFuseMountsImpl(isFuse, root, exe, mounts)
}

func resolveFuseMountsImpl(fuseCheck func(path string) bool, root, exe string, mounts []Mount) []Mount {
	// If we are not in a FUSE filesystem, we don't need to do anything.
	if !fuseCheck(root) {
		return mounts
	}

	seen := make(map[string]bool)
	resolvedMounts := make([]Mount, 0, len(mounts)+1)

	// Process all mounts, plus the executable path itself. The executable might
	// be a symlink or reside in a symlinked directory that needs to be mounted
	// differently than its parent.
	mountsToProcess := make([]Mount, len(mounts), len(mounts)+1)
	copy(mountsToProcess, mounts)

	if exe != "" {
		mountsToProcess = append(mountsToProcess, Mount{
			Path: exe,
			// ReadOnly is safe for the executable
			Writable: false,
		})
	}

	addMount := func(path, dest string, writable bool) {
		if !seen[dest] {
			seen[dest] = true
			resolvedMounts = append(resolvedMounts, Mount{
				Path:     path,
				Dest:     dest,
				Writable: writable,
			})
		}
	}

	for _, m := range mountsToProcess {
		// If the mount is outside the root, we shouldn't attempt to resolve it
		// or its parents.
		if rel, err := filepath.Rel(root, m.Path); err != nil || strings.HasPrefix(rel, "..") {
			dest := m.Dest
			if dest == "" {
				dest = m.Path
			}
			addMount(m.Path, dest, m.Writable)
			continue
		}

		// Walk up the tree to see if any of the parents are a symlink.
		// This is crucial for tools (like python) that rely on sibling directories
		// (e.g. ../lib) to be present relative to the binary. If we only resolved
		// the final path, we might mount the binary file itself but not the
		// surrounding directory structure that contains necessary libraries.
		// Stop if we reach the root or the current directory (".") in the case of relative paths.
		for path := m.Path; path != root && path != "."; path = filepath.Dir(path) {
			info, err := os.Lstat(path)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				// We found a symlink, so we need to resolve it and mount the destination.
				realPath, err := filepath.EvalSymlinks(path)
				if err != nil {
					// If we can't resolve the symlink, just skip it.
					continue
				}

				target := filepath.Clean(realPath)

				// Mount the real path at its real location. This ensures that
				// the symlink target actually exists in the sandbox. If we only
				// mounted the overlay, the symlink itself (if accessed via another Path)
				// might still be broken if its target is missing.
				addMount(target, target, m.Writable)

				// Mount the real path at the symlink's location. This effectively
				// "overlays" the real content onto the FUSE path. This is necessary
				// because mounting directly from a FUSE source can be problematic
				// (e.g. with MS_RDONLY remounts), and because we want to ensure
				// the directory structure (siblings) is preserved via the real content.
				addMount(target, path, m.Writable)
			}
		}

		// Handle the leaf path itself.
		// The loop above handles parent directories (fixing the environment/siblings),
		// but it doesn't ensure the *leaf* itself is mounted if the leaf is not
		// a symlink (or if it IS a symlink but had no parent symlinks).
		// We must always process the leaf to:
		// 1. Respect the user's requested `Dest` (which the loop doesn't see).
		// 2. Handle if the leaf itself is a symlink.
		// 3. Ensure the leaf is mounted from its physical path.
		realPath, err := filepath.EvalSymlinks(m.Path)
		if err != nil {
			// If we can't resolve the symlink, just keep the original mount.
			dest := m.Dest
			if dest == "" {
				dest = m.Path
			}
			addMount(m.Path, dest, m.Writable)
			continue
		}

		target := filepath.Clean(realPath)

		// Ensure RealPath is mounted at RealPath (Source=Real, Dest=Real).
		addMount(target, target, m.Writable)

		// Ensure RealPath is mounted at Requested Destination (Source=Real, Dest=Requested).
		dest := m.Dest
		if dest == "" {
			dest = m.Path
		}
		dest = filepath.Clean(dest)

		addMount(target, dest, m.Writable)
	}
	return resolvedMounts
}
