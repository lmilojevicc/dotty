//go:build darwin && cgo

package nativefs

/*
#include <sys/types.h>
#include <sys/acl.h>
#include <sys/attr.h>
#include <unistd.h>
#include <errno.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

_Static_assert(sizeof(uint32_t) == 4, "attribute length header");
_Static_assert(sizeof(attrreference_t) == 8, "public attrreference layout");
_Static_assert(offsetof(attrreference_t, attr_dataoffset) == 0, "reference offset field");
_Static_assert(offsetof(attrreference_t, attr_length) == 4, "reference length field");
_Static_assert(sizeof(((attrreference_t *)0)->attr_dataoffset) == sizeof(int32_t), "signed offset width");
_Static_assert(_Generic(((attrreference_t *)0)->attr_dataoffset, int32_t: 1, default: 0), "signed offset type");
_Static_assert(sizeof(((attrreference_t *)0)->attr_length) == sizeof(uint32_t), "reference length width");
_Static_assert(_Generic(((attrreference_t *)0)->attr_length, uint32_t: 1, default: 0), "unsigned reference length type");

enum {
	DOTTY_ATTR_HEADER_SIZE = sizeof(uint32_t),
	DOTTY_ATTR_FIXED_SIZE = sizeof(uint32_t) + sizeof(attrreference_t),
	DOTTY_ATTR_EMPTY_OFFSET = sizeof(attrreference_t)
};

// Public required-attribute contract and pinned XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea
// (12377.1.9), corroborated by the private APFS probe; this is not an exact
// source mapping for runtime 12377.91.3. See the maintained probe README.
// This required-attribute request must not use RETURNED_ATTRS or PACK_INVAL:
// an unavailable required va_acl must fail, not be packed as optional absence.
// Only the fixed empty shape is interpreted; no reference is ever followed.
typedef struct {
	int code, header, fields;
	uint32_t capacity, length, ref_length;
	int32_t ref_offset;
} dotty_required_acl_result;

static dotty_required_acl_result dotty_required_acl(int fd) {
	dotty_required_acl_result r = {0};
	struct attrlist request = {0};
	request.bitmapcount = ATTR_BIT_MAP_COUNT;
	request.commonattr = ATTR_CMN_EXTENDED_SECURITY;
	uint32_t buffer[1024] = {0};
	r.capacity = sizeof(buffer);
	errno = 0;
	int status = fgetattrlist(fd, &request, buffer, sizeof(buffer), FSOPT_REPORT_FULLSIZE);
	int saved = errno;
	if (status != 0) {
		r.code = saved ? saved : EIO;
		return r;
	}
	// Successful calls need not clear errno. FULLSIZE may report success even
	// when only the header fits; check capacity and full length independently.
	if (r.capacity < DOTTY_ATTR_HEADER_SIZE) return r;
	memcpy(&r.length, buffer, sizeof(r.length));
	r.header = 1;
	if (r.capacity < DOTTY_ATTR_FIXED_SIZE || r.length < DOTTY_ATTR_FIXED_SIZE ||
		r.length > r.capacity) return r;
	attrreference_t ref;
	memcpy(&ref, (unsigned char *)buffer + DOTTY_ATTR_HEADER_SIZE, sizeof(ref));
	r.ref_offset = ref.attr_dataoffset;
	r.ref_length = ref.attr_length;
	r.fields = 1;
	return r;
}

// All ABI types/constants come from the public SDK. Capture errno in the same
// C call, before any subsequent library call can overwrite it.
typedef struct {
	int value;
	int code;
	int getter_errno;
} dotty_acl_result;

typedef struct {
	acl_t acl;
	int code;
} dotty_acl_open_result;

typedef struct {
	int present;
	int code;
	dotty_acl_result inherited;
} dotty_acl_entry_result;

static dotty_acl_open_result dotty_acl_get(int fd) {
	errno = 0;
	acl_t acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED);
	int saved = errno;
	dotty_acl_open_result result = {acl, 0};
	if (acl == NULL) result.code = saved ? saved : EIO;
	return result;
}

// Heap-only fixture allocation; used solely by the allocated-empty ACL test.
// This cannot set or clear filesystem ACLs and is not a public package API.
static dotty_acl_open_result dotty_acl_empty_for_test(void) {
	errno = 0;
	acl_t acl = acl_init(0);
	int saved = errno;
	dotty_acl_open_result result = {acl, 0};
	if (acl == NULL) result.code = saved ? saved : EIO;
	return result;
}

static dotty_acl_result dotty_acl_validate(acl_t acl) {
	errno = 0;
	int value = acl_valid(acl);
	int saved = errno;
	dotty_acl_result result = {value, 0, 0};
	if (value != 0) result.code = saved ? saved : EIO;
	return result;
}

static dotty_acl_result dotty_acl_release(acl_t acl) {
	errno = 0;
	int value = acl_free(acl);
	int saved = errno;
	dotty_acl_result result = {value, 0, 0};
	if (value != 0) result.code = saved ? saved : EIO;
	return result;
}

// obj must be a validated, live ACL or its borrowed entry, and flag must be a
// single public flag valid for that object. acl_get_flag_np does not validate its
// pointer, and its normal 0/1 results do not set or clear errno. The flagset is
// borrowed from obj and must NOT be freed independently.
static dotty_acl_result dotty_acl_flag(void *obj, acl_flag_t flag) {
	acl_flagset_t flags = NULL;
	errno = 0;
	int value = acl_get_flagset_np(obj, &flags);
	int saved = errno;
	dotty_acl_result result = {0, 0, 0};
	if (value != 0) {
		result.code = saved ? saved : EIO;
		return result;
	}
	if (flags == NULL) {
		result.code = EIO;
		return result;
	}
	result.value = acl_get_flag_np(flags, flag);
	result.getter_errno = errno;
	return result;
}

static dotty_acl_result dotty_acl_deferred(acl_t acl) {
	return dotty_acl_flag(acl, ACL_FLAG_DEFER_INHERIT);
}

static dotty_acl_entry_result dotty_acl_first(acl_t acl) {
	dotty_acl_entry_result result = {0, 0, {0, 0, 0}};
	// EINVAL alone is ambiguous. Revalidate this owned ACL immediately before
	// interpreting FIRST_ENTRY's -1/EINVAL as exhaustion of a valid empty list.
	dotty_acl_result valid = dotty_acl_validate(acl);
	if (valid.code != 0) {
		result.code = valid.code;
		return result;
	}
	acl_entry_t entry = NULL;
	errno = 0;
	int value = acl_get_entry(acl, ACL_FIRST_ENTRY, &entry);
	int saved = errno;
	if (value == -1 && saved == EINVAL) return result;
	if (value != 0 || entry == NULL) {
		result.code = saved ? saved : EIO;
		return result;
	}
	// Darwin's successful entry result is 0. Any entry refuses, independent of
	// its allow/deny permissions; flags retain native inherited-entry evidence.
	result.present = 1;
	result.inherited = dotty_acl_flag(entry, ACL_ENTRY_INHERITED);
	return result;
}
*/
import "C"

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// Getter semantics were checked against public Apple Libc-1752.100.10:
// https://github.com/apple-oss-distributions/Libc/blob/71bbe350ab79eef58113991d817ccc6165061a64/posix1e/acl_flag.c
// The installed OS's exact correspondence to that source is not established;
// required native Darwin tests remain an independent gate, not a source claim.
type darwinACL struct {
	value C.acl_t
}

type darwinACLCalls struct {
	get      func(int) (*darwinACL, error)
	valid    func(*darwinACL) error
	deferred func(*darwinACL) (bool, error)
	first    func(*darwinACL) (bool, error)
	free     func(*darwinACL) error
	confirm  func(int) (darwinRequiredACLResult, error)
}

const darwinRequiredACLMechanism = "acl_get_fd_np:extended+fgetattrlist:required-extended-security"

// Fixed SDK fields only, never a raw filesec or variable-length ACL parser.
type darwinRequiredACLResult struct {
	capacity  uint32
	length    uint32
	offset    int32
	refLength uint32
	header    bool
	fields    bool
}

func (r darwinRequiredACLResult) confirmsAbsence() bool {
	return r.header && r.fields && r.capacity >= uint32(C.DOTTY_ATTR_FIXED_SIZE) &&
		r.length <= r.capacity && r.length == uint32(C.DOTTY_ATTR_FIXED_SIZE) &&
		r.offset == int32(C.DOTTY_ATTR_EMPTY_OFFSET) && r.refLength == 0
}

func darwinACLOpenResult(op string, value C.acl_t, code int) (*darwinACL, error) {
	if value != nil {
		return &darwinACL{value: value}, nil // Ignore errno after allocated success.
	}
	if code == 0 {
		code = int(unix.EIO)
	}
	return nil, darwinACLCallError(op, C.int(code))
}

func allocateDarwinEmptyACLForTest() (*darwinACL, error) {
	result := C.dotty_acl_empty_for_test()
	return darwinACLOpenResult("acl_init test fixture", result.acl, int(result.code))
}

type darwinACLEntry struct {
	present   bool
	inherited bool
}

func darwinACLCallError(op string, code C.int) error {
	if code == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", op, unix.Errno(code))
}

func darwinFlagResult(value int, capturedErrno unix.Errno) (bool, error) {
	switch value {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		// No documented -1/error path exists in the cited getter. Preserve an
		// unexpected result as failure, never attribute stale errno to it.
		return false, fmt.Errorf(
			"unexpected acl_get_flag_np result %d (unattributed errno %d): %w",
			value,
			int(capturedErrno),
			unix.EIO,
		)
	}
}

func darwinFirstEntry(acl *darwinACL) (darwinACLEntry, error) {
	result := C.dotty_acl_first(acl.value)
	if err := darwinACLCallError("validate/enumerate ACL entry", result.code); err != nil {
		return darwinACLEntry{}, err
	}
	if result.present == 0 {
		return darwinACLEntry{}, nil
	}
	if err := darwinACLCallError("read ACL entry flagset", result.inherited.code); err != nil {
		return darwinACLEntry{}, err
	}
	inherited, err := darwinFlagResult(
		int(result.inherited.value),
		unix.Errno(result.inherited.getter_errno),
	)
	return darwinACLEntry{present: true, inherited: inherited}, err
}

func systemDarwinACLCalls() darwinACLCalls {
	return darwinACLCalls{
		get: func(fd int) (*darwinACL, error) {
			result := C.dotty_acl_get(C.int(fd))
			return darwinACLOpenResult("acl_get_fd_np", result.acl, int(result.code))
		},
		valid: func(acl *darwinACL) error {
			result := C.dotty_acl_validate(acl.value)
			return darwinACLCallError("acl_valid", result.code)
		},
		deferred: func(acl *darwinACL) (bool, error) {
			result := C.dotty_acl_deferred(acl.value)
			if err := darwinACLCallError("read ACL-level flagset", result.code); err != nil {
				return false, err
			}
			return darwinFlagResult(int(result.value), unix.Errno(result.getter_errno))
		},
		first: func(acl *darwinACL) (bool, error) {
			entry, err := darwinFirstEntry(acl)
			return entry.present, err
		},
		free: func(acl *darwinACL) error {
			result := C.dotty_acl_release(acl.value)
			return darwinACLCallError("acl_free", result.code)
		},
		confirm: func(fd int) (darwinRequiredACLResult, error) {
			r := C.dotty_required_acl(C.int(fd))
			return darwinRequiredACLResult{
				capacity: uint32(r.capacity), length: uint32(r.length),
				offset: int32(r.ref_offset), refLength: uint32(r.ref_length),
				header: r.header != 0, fields: r.fields != 0,
			}, darwinACLCallError("fgetattrlist required extended security", r.code)
		},
	}
}

func readDarwinSecurity(fd int, calls darwinACLCalls) (evidence SecurityEvidence, resultErr error) {
	evidence = SecurityEvidence{state: EvidenceFailed, mechanism: "acl_get_fd_np:extended"}
	acl, err := calls.get(fd)
	if acl != nil {
		defer func() {
			if err := calls.free(acl); err != nil {
				evidence.state = EvidenceFailed
				resultErr = errors.Join(resultErr, err)
			}
		}()
	}
	fail := func(err error) (SecurityEvidence, error) {
		// Darwin ENOTSUP and EOPNOTSUPP are distinct errno values. Match the
		// native capability classifier, including wrapped syscall errors.
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP) ||
			errors.Is(err, unix.EOPNOTSUPP) {
			evidence.state = EvidenceUnsupported
		}
		return evidence, err
	}
	if acl == nil && errors.Is(err, unix.ENOENT) {
		evidence.mechanism = darwinRequiredACLMechanism
		confirmation, confirmErr := calls.confirm(fd)
		if confirmErr != nil {
			return fail(confirmErr)
		}
		if !confirmation.confirmsAbsence() {
			return fail(
				fmt.Errorf(
					"required extended-security absence unproven: %+v: %w",
					confirmation,
					unix.EIO,
				),
			)
		}
		evidence.state = EvidenceAbsent
		return evidence, nil
	}
	if err != nil {
		return fail(err)
	}
	if acl == nil {
		return fail(fmt.Errorf("acl_get_fd_np returned no owned ACL: %w", unix.EIO))
	}
	if err := calls.valid(acl); err != nil {
		return fail(err)
	}
	deferred, err := calls.deferred(acl)
	if err != nil {
		return fail(err)
	}
	if deferred {
		evidence.state = EvidencePresent
		return evidence, nil
	}
	present, err := calls.first(acl)
	if err != nil {
		return fail(err)
	}
	if present {
		evidence.state = EvidencePresent
	} else {
		evidence.state = EvidenceAbsent
	}
	return evidence, nil
}
