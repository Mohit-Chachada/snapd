// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	// btrfsSubvolumeIno is the fixed inode number for the root directory of
	// any btrfs subvolume. This is a kernel guarantee.
	btrfsSubvolumeIno = 256

	// btrfsPathNameMax is BTRFS_PATH_NAME_MAX from <linux/btrfs.h>.
	btrfsPathNameMax = 4087

	// btrfsIocSnapDestroy is BTRFS_IOC_SNAP_DESTROY from <linux/btrfs.h>,
	// computed as _IOW(0x94, 15, struct btrfs_ioctl_vol_args).
	// sizeof(btrfs_ioctl_vol_args) = 8 (s64 fd) + 4088 (name) = 4096 = 0x1000.
	// _IOW = (1<<30) | (size<<16) | (type<<8) | nr
	//      = 0x40000000 | 0x10000000 | 0x00009400 | 0x0f = 0x5000940f.
	btrfsIocSnapDestroy = uintptr(0x5000940f)
)

// btrfsIoctlVolArgs mirrors struct btrfs_ioctl_vol_args from <linux/btrfs.h>.
type btrfsIoctlVolArgs struct {
	fd   int64
	name [btrfsPathNameMax + 1]byte
}

// isBtrfsSubvolume reports whether path is the root of a btrfs subvolume.
// The root directory of every btrfs subvolume always has inode number 256;
// this is a kernel guarantee.
// This is a variable so it can be replaced in tests.
var isBtrfsSubvolume = func(path string) (bool, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return false, err
	}
	return st.Ino == btrfsSubvolumeIno, nil
}

// btrfsSnapDestroy deletes the btrfs subvolume named name directly under
// parentDir via BTRFS_IOC_SNAP_DESTROY.
// This is a variable so it can be replaced in tests.
var btrfsSnapDestroy = func(parentDir, name string) error {
	if len(name) > btrfsPathNameMax {
		return fmt.Errorf("subvolume name %q exceeds maximum length %d", name, btrfsPathNameMax)
	}

	f, err := os.Open(parentDir)
	if err != nil {
		return err
	}
	defer f.Close()

	var args btrfsIoctlVolArgs
	copy(args.name[:], name)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
		f.Fd(),
		btrfsIocSnapDestroy,
		uintptr(unsafe.Pointer(&args)),
	)
	if errno != 0 {
		return os.NewSyscallError("ioctl(BTRFS_IOC_SNAP_DESTROY)", errno)
	}
	return nil
}

// removeBtrfsSubvolsIn recursively deletes all btrfs subvolumes nested under
// dir, deepest first, via BTRFS_IOC_SNAP_DESTROY. Regular directories are
// traversed but not deleted here; os.RemoveAll handles them afterwards.
func removeBtrfsSubvolsIn(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Skip unreadable or missing directories; os.RemoveAll will surface
		// the real error if the path itself is the problem.
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p := filepath.Join(dir, entry.Name())
		isSubvol, err := isBtrfsSubvolume(p)
		if err != nil {
			continue
		}
		// Always recurse first so nested subvolumes are destroyed before
		// their parent subvolume.
		if err := removeBtrfsSubvolsIn(p); err != nil {
			return err
		}
		if isSubvol {
			if err := btrfsSnapDestroy(dir, entry.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveAllBtrfs removes path and everything under it. Unlike os.RemoveAll, it
// handles btrfs subvolumes nested under path by deleting them deepest-first
// via BTRFS_IOC_SNAP_DESTROY before calling os.RemoveAll on the root. On
// filesystems without btrfs subvolumes the behaviour is identical to
// os.RemoveAll.
func RemoveAllBtrfs(path string) error {
	if err := removeBtrfsSubvolsIn(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
