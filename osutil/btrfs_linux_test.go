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

package osutil_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type btrfsSuite struct{}

var _ = Suite(&btrfsSuite{})

func (s *btrfsSuite) TestRemoveAllBtrfsNonExistent(c *C) {
	// os.RemoveAll on a non-existent path returns nil; so should we.
	err := osutil.RemoveAllBtrfs(filepath.Join(c.MkDir(), "does-not-exist"))
	c.Assert(err, IsNil)
}

func (s *btrfsSuite) TestRemoveAllBtrfsRegularTree(c *C) {
	// No subvolumes: behaves exactly like os.RemoveAll.
	dir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(dir, "a", "b"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "a", "b", "file"), []byte("x"), 0644), IsNil)

	c.Assert(osutil.RemoveAllBtrfs(dir), IsNil)
	c.Assert(osutil.FileExists(dir), Equals, false)
}

func (s *btrfsSuite) TestRemoveAllBtrfsCallsSnapDestroy(c *C) {
	dir := c.MkDir()
	subvol := filepath.Join(dir, "subvol")
	c.Assert(os.MkdirAll(subvol, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(subvol, "file"), []byte("data"), 0644), IsNil)

	var destroyedParent, destroyedName string
	defer osutil.MockBtrfsSnapDestroy(func(parent, name string) error {
		destroyedParent = parent
		destroyedName = name
		return os.RemoveAll(filepath.Join(parent, name))
	})()
	defer osutil.MockIsBtrfsSubvolume(func(path string) (bool, error) {
		return path == subvol, nil
	})()

	c.Assert(osutil.RemoveAllBtrfs(dir), IsNil)
	c.Assert(destroyedParent, Equals, dir)
	c.Assert(destroyedName, Equals, "subvol")
	c.Assert(osutil.FileExists(dir), Equals, false)
}

func (s *btrfsSuite) TestRemoveAllBtrfsNestedSubvolumesDeepestFirst(c *C) {
	// Nested subvolumes must be destroyed inner-first so that the parent
	// subvolume is empty when BTRFS_IOC_SNAP_DESTROY is called on it.
	dir := c.MkDir()
	outerSubvol := filepath.Join(dir, "outer")
	innerSubvol := filepath.Join(outerSubvol, "inner")
	c.Assert(os.MkdirAll(innerSubvol, 0755), IsNil)

	var destroyed []string
	defer osutil.MockBtrfsSnapDestroy(func(parent, name string) error {
		destroyed = append(destroyed, filepath.Join(parent, name))
		return os.RemoveAll(filepath.Join(parent, name))
	})()

	subvols := map[string]bool{outerSubvol: true, innerSubvol: true}
	defer osutil.MockIsBtrfsSubvolume(func(path string) (bool, error) {
		return subvols[path], nil
	})()

	c.Assert(osutil.RemoveAllBtrfs(dir), IsNil)
	c.Assert(destroyed, HasLen, 2)
	c.Assert(destroyed[0], Equals, innerSubvol)
	c.Assert(destroyed[1], Equals, outerSubvol)
	c.Assert(osutil.FileExists(dir), Equals, false)
}

func (s *btrfsSuite) TestRemoveAllBtrfsSnapDestroyError(c *C) {
	dir := c.MkDir()
	subvol := filepath.Join(dir, "subvol")
	c.Assert(os.MkdirAll(subvol, 0755), IsNil)

	defer osutil.MockBtrfsSnapDestroy(func(parent, name string) error {
		return os.NewSyscallError("ioctl(BTRFS_IOC_SNAP_DESTROY)", os.ErrPermission)
	})()
	defer osutil.MockIsBtrfsSubvolume(func(path string) (bool, error) {
		return path == subvol, nil
	})()

	err := osutil.RemoveAllBtrfs(dir)
	c.Assert(err, ErrorMatches, ".*ioctl.*permission denied")
}
