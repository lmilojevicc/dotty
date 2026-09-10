package nativefs

import "fmt"

// EvidenceState distinguishes an observation from unavailable evidence. The zero
// value never means absence. Present filesystem evidence means the descriptor's
// filesystem/mount meets this slice's bounded model; present ACLs always refuse.
type EvidenceState uint8

const (
	EvidenceInvalid EvidenceState = iota
	EvidenceAbsent
	EvidencePresent
	EvidenceUnsupported
	EvidenceFailed
)

// SecurityEvidence is an immutable descriptor observation, not ACL evaluation.
// Absence means all ACL sources relevant to this platform and object were read.
type SecurityEvidence struct {
	state     EvidenceState
	mechanism string
}

func (e SecurityEvidence) State() EvidenceState { return e.state }
func (e SecurityEvidence) Mechanism() string    { return e.mechanism }

// FilesystemFacts retain the descriptor-observed filesystem ID, type, and mount
// flags used by the native policy. These are observations, not a mount lease.
type FilesystemFacts struct {
	state EvidenceState
	model string
	kind  uint64
	flags uint64
	id    string
}

func (f FilesystemFacts) State() EvidenceState { return f.state }
func (f FilesystemFacts) Model() string        { return f.model }
func (f FilesystemFacts) Type() uint64         { return f.kind }
func (f FilesystemFacts) Flags() uint64        { return f.flags }
func (f FilesystemFacts) ID() string           { return f.id }

// AuthorityFacts deliberately do not extend Identity. No fact grants deletion,
// adoption, or future mutation authority; guarded operations must reobserve it.
// Values and all returned observations are immutable copies with no setters.
type AuthorityFacts struct {
	identity   Identity
	uid        uint32
	gid        uint32
	mode       uint32
	nlink      uint64
	filesystem FilesystemFacts
	security   SecurityEvidence
}

func (f AuthorityFacts) Identity() Identity          { return f.identity }
func (f AuthorityFacts) UID() uint32                 { return f.uid }
func (f AuthorityFacts) GID() uint32                 { return f.gid }
func (f AuthorityFacts) Mode() uint32                { return f.mode }
func (f AuthorityFacts) LinkCount() uint64           { return f.nlink }
func (f AuthorityFacts) Filesystem() FilesystemFacts { return f.filesystem }
func (f AuthorityFacts) Security() SecurityEvidence  { return f.security }

// AuthorityRole is an operation's required policy, not a caller's assertion
// about the object. System ancestors are validation-only, never flock targets.
type AuthorityRole uint8

const (
	RolePrivateAnchor AuthorityRole = iota + 1
	RoleLockFile
	RoleRepositoryDirectory
	RoleSystemAncestor
)

const (
	ReasonAuthorityPolicy  Reason = "unsafe-authority"
	ReasonAuthorityChanged Reason = "authority-changed"
)

// AuthorityError retains exact observation paths, role, facts and recovery
// guidance. It is ordinary primitive evidence, never a final C4 classification.
type AuthorityError struct {
	Op          string
	Path        string
	Role        AuthorityRole
	Reason      Reason
	Expected    AuthorityFacts
	Observed    AuthorityFacts
	Detail      string
	Remediation string
	Err         error
}

func (e *AuthorityError) Error() string {
	text := fmt.Sprintf("nativefs %s: %s: path=%q role=%d: %s; expected=%+v observed=%+v; %s",
		e.Op, e.Reason, e.Path, e.Role, e.Detail, e.Expected, e.Observed, e.Remediation)
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}
	return text
}

func (e *AuthorityError) Unwrap() error { return e.Err }
