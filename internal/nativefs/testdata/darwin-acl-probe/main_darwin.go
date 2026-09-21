//go:build darwin && cgo

// This test-only executable reports fixed fields, never ACL payloads or GUIDs.
package main

/*
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/attr.h>
#include <unistd.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>

_Static_assert(sizeof(attrreference_t) == 8, "public attrreference layout");

typedef struct {
	int status, code, stat_before, stat_after, identity_stable;
	uint64_t device, inode;
	uint32_t mode, capacity, length, ref_length;
	int32_t ref_offset;
	int fields_valid, complete;
} probe_result;

static probe_result probe(int variant) {
	probe_result r = {0};
	struct stat before = {0}, after = {0};
	errno = 0;
	if (fstat(3, &before) != 0) {
		r.stat_before = errno;
		return r;
	}
	r.device = (uint64_t)before.st_dev;
	r.inode = (uint64_t)before.st_ino;
	r.mode = before.st_mode;

	struct attrlist request = {0};
	request.bitmapcount = ATTR_BIT_MAP_COUNT;
	request.commonattr = ATTR_CMN_EXTENDED_SECURITY;
	// A fixed, aligned buffer. FULLSIZE detects truncation; never retry or
	// follow the reference into the variable-length security payload.
	uint32_t buffer[1024] = {0};
	r.capacity = sizeof(buffer);
	int fd = 3;
	if (variant == 1) fd = -1;
	if (variant == 2) r.capacity = sizeof(uint32_t) - 1;
	if (variant == 3) request.bitmapcount = 0;
	if (variant == 4) r.capacity = sizeof(uint32_t);
	errno = 0;
	r.status = fgetattrlist(fd, &request, buffer, r.capacity, FSOPT_REPORT_FULLSIZE);
	r.code = errno;
	if (r.status == 0 && r.capacity >= sizeof(uint32_t)) {
		memcpy(&r.length, buffer, sizeof(r.length));
		if (r.capacity >= 12 && r.length >= 12) {
			attrreference_t ref;
			memcpy(&ref, (unsigned char *)buffer + sizeof(uint32_t), sizeof(ref));
			r.ref_offset = ref.attr_dataoffset;
			r.ref_length = ref.attr_length;
			r.fields_valid = 1;
			r.complete = r.length <= r.capacity;
		}
	}
	errno = 0;
	if (fstat(3, &after) != 0) r.stat_after = errno;
	else r.identity_stable = before.st_dev == after.st_dev &&
		before.st_ino == after.st_ino && before.st_mode == after.st_mode &&
		before.st_uid == after.st_uid && before.st_gid == after.st_gid &&
		before.st_nlink == after.st_nlink && before.st_size == after.st_size &&
		before.st_flags == after.st_flags &&
		before.st_mtimespec.tv_sec == after.st_mtimespec.tv_sec &&
		before.st_mtimespec.tv_nsec == after.st_mtimespec.tv_nsec &&
		before.st_ctimespec.tv_sec == after.st_ctimespec.tv_sec &&
		before.st_ctimespec.tv_nsec == after.st_ctimespec.tv_nsec;
	return r;
}
*/
import "C"

import (
	"encoding/json"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	variants := map[string]int{
		"normal":           0,
		"invalid-fd":       1,
		"short-buffer":     2,
		"invalid-request":  3,
		"truncated-buffer": 4,
	}
	variant, ok := variants[os.Args[1]]
	if !ok {
		os.Exit(2)
	}
	r := C.probe(C.int(variant))
	// Exit status describes the protocol, not syscall success. Errors must be
	// observable as status/errno, never converted into absence by the launcher.
	err := json.NewEncoder(os.Stdout).Encode(struct {
		Status         int
		Errno          int
		StatBefore     int
		StatAfter      int
		IdentityStable bool
		Device         uint64
		Inode          uint64
		Mode           uint32
		Capacity       uint32
		Length         uint32
		RefOffset      int32
		RefLength      uint32
		FieldsValid    bool
		Complete       bool
	}{
		Status: int(r.status), Errno: int(r.code),
		StatBefore: int(r.stat_before), StatAfter: int(r.stat_after),
		IdentityStable: r.identity_stable != 0,
		Device: uint64(r.device), Inode: uint64(r.inode), Mode: uint32(r.mode),
		Capacity: uint32(r.capacity), Length: uint32(r.length),
		RefOffset: int32(r.ref_offset), RefLength: uint32(r.ref_length),
		FieldsValid: r.fields_valid != 0, Complete: r.complete != 0,
	})
	if err != nil {
		os.Exit(1)
	}
}
