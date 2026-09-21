package nativefs

import "fmt"

const (
	ReasonMetadataChanged Reason = "regular-metadata-changed"
	ReasonInvalidMetadata Reason = "invalid-regular-metadata"
)

// regularMetadata is comparable: every baseline field, including identity
// validity, participates in equality. Times are exact native seconds/nanoseconds.
// Atime is deliberately absent; reading may change it even on failure.
type regularMetadata struct {
	identity            Identity
	uid, gid            uint32
	mode                uint32
	nlink               uint64
	size                int64
	mtimeSec, mtimeNsec int64
	ctimeSec, ctimeNsec int64
}

// RegularObservation describes bounded regular-file metadata and bytes read,
// not an atomic/durable snapshot, complete fidelity, or filesystem authority.
// Its zero value is invalid. Value getters never expose a descriptor or reader.
type RegularObservation struct {
	metadata regularMetadata
	digest   [32]byte
	valid    bool
}

func (o RegularObservation) Valid() bool        { return o.valid }
func (o RegularObservation) Identity() Identity { return o.metadata.identity }
func (o RegularObservation) UID() uint32        { return o.metadata.uid }
func (o RegularObservation) GID() uint32        { return o.metadata.gid }

// Mode returns only permission and special bits (07777).
func (o RegularObservation) Mode() uint32      { return o.metadata.mode }
func (o RegularObservation) LinkCount() uint64 { return o.metadata.nlink }
func (o RegularObservation) Size() int64       { return o.metadata.size }
func (o RegularObservation) Mtime() (seconds, nanoseconds int64) {
	return o.metadata.mtimeSec, o.metadata.mtimeNsec
}

func (o RegularObservation) Ctime() (seconds, nanoseconds int64) {
	return o.metadata.ctimeSec, o.metadata.ctimeNsec
}
func (o RegularObservation) SHA256() [32]byte { return o.digest }

// RegularObservationError retains exact comparison evidence and primitive
// causes, never a mutation outcome. Expected and Observed contain metadata only:
// their Valid getters are false and their digests zero, not successful reads.
// Malformed native metadata, when available, is retained in Observed as evidence.
type RegularObservationError struct {
	Path               string
	Component          string
	Reason             Reason
	Expected, Observed RegularObservation
	Detail             string
	Remediation        string
	Err                error
}

func (e *RegularObservationError) Error() string {
	text := fmt.Sprintf(
		"nativefs observe-regular: %s: path=%q component=%q expected=%+v observed=%+v",
		e.Reason,
		e.Path,
		e.Component,
		e.Expected.metadata,
		e.Observed.metadata,
	)
	if e.Detail != "" {
		text += ": " + e.Detail
	}
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}
	if e.Remediation != "" {
		text += "; " + e.Remediation
	}
	return text
}

func (e *RegularObservationError) Unwrap() error { return e.Err }

func regularError(
	path string,
	name Component,
	reason Reason,
	detail string,
	expected, observed regularMetadata,
	cause error,
) *RegularObservationError {
	return &RegularObservationError{
		Path:      path,
		Component: name.name,
		Reason:    reason,
		Expected: RegularObservation{
			metadata: expected,
		},
		Observed:    RegularObservation{metadata: observed},
		Detail:      detail,
		Err:         cause,
		Remediation: "Stop concurrent edits and inspect the affected path and observation error; use a supported readable regular file without weakening path checks.",
	}
}

func normalizeRegularMetadata(
	id Identity,
	uid, gid, mode uint32,
	nlink uint64,
	size, mtimeSec, mtimeNsec, ctimeSec, ctimeNsec int64,
) (regularMetadata, error) {
	m := regularMetadata{
		identity: id, uid: uid, gid: gid, mode: mode, nlink: nlink, size: size,
		mtimeSec: mtimeSec, mtimeNsec: mtimeNsec, ctimeSec: ctimeSec, ctimeNsec: ctimeNsec,
	}
	// S_IFREG is the same Unix ABI value on the supported data backends. This
	// common normalization uses no platform I/O and also compiles in foreign stubs.
	if !id.Valid || id.Kind != 0o100000 || mode & ^uint32(0o7777) != 0 || size < 0 ||
		mtimeNsec < 0 ||
		mtimeNsec >= 1000000000 ||
		ctimeNsec < 0 ||
		ctimeNsec >= 1000000000 {
		return regularMetadata{}, fmt.Errorf("unusable regular metadata: %+v", m)
	}
	return m, nil
}

func compareRegularMetadata(path string, name Component, expected, observed regularMetadata) error {
	if expected == observed {
		return nil
	}
	reason := ReasonMetadataChanged
	if expected.identity != observed.identity {
		reason = ReasonIdentityChanged
	}
	return regularError(
		path,
		name,
		reason,
		"regular-file baseline changed",
		expected,
		observed,
		nil,
	)
}
