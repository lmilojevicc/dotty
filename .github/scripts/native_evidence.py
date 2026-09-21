"""Source-bound native inventories and strict parsers adapted from the audited K core.

No transport or runtime controller. configure() binds original log path grammar;
raw evidence is never rewritten. See README.md for the reuse/adaptation map.
"""
from collections import Counter
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import stat

OUTPUT_LIMIT = 8 * 1024 * 1024
PRIVATE_TMP = None
PRIVATE_UID = None

# Required root cases are independent per aggregator. Nested cases never inflate root counts.
INVENTORIES = {'TestPrivateCreateOpenBoundary': ['ExclusiveFileModeAfterSecurityBinding',
                                   'ExistingObjectNeverRepairedTruncatedRemoved',
                                   'RawOpenFlags',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused',
                                   'LockHardlinkRefused',
                                   'CreateCollisionWinnerUntouched',
                                   'MkdirCreationNotInodeOwnership',
                                   'PostCreateReplacementRefused',
                                   'RestrictiveUmaskDirectoryRetained',
                                   'CreationRecordNoDeleteAuthority',
                                   'ExactRecoveryPaths',
                                   'SystemAncestorsValidationOnly'],
 'TestAuthorityBoundary': ['ZeroEvidenceInvalid',
                           'IdentityNotAuthority',
                           'DescriptorFactsUIDGIDModeSpecialNlink',
                           'RolePolicyMatrix',
                           'ACLAbsentVerified',
                           'ACLPresentRefused',
                           'ACLReadFailureRefused',
                           'FilesystemMountUnknownRefused',
                           'SecurityDriftSameInode',
                           'FullIdentityTopologyAncestry',
                           'PrivateSecurityBoundaryOnly',
                           'UnboundedProductionPolicyRefusal'],
 'TestLinuxAuthority': ['PosixAccessENODATA',
                        'DirectoryDefaultENODATA',
                        'AccessACLPresentRefused',
                        'DefaultACLPresentRefused',
                        'EOPNOTSUPPNotAbsence',
                        'DescriptorExtTmpfsVerified',
                        'NfsCifsFuseOverlayUnknownRefused',
                        'ReadonlyOverlayStillRefused'],
 'TestNativeHandleBoundary': ['root',
                              'normal',
                              'missing_ancestry',
                              'nondirectory_ancestry',
                              'ancestor_symlink',
                              'final_directory_symlink',
                              'physical_path_syntax',
                              'leaf_symlink',
                              'invalid_components',
                              'zero_identity',
                              'missing_source',
                              'absent_destination',
                              'absent_directory_destination',
                              'existing_file',
                              'existing_directory',
                              'existing_dangling_symlink',
                              'source_swap',
                              'source_parent_swap',
                              'destination_parent_swap',
                              'ancestor_swap',
                              'directory_into_self',
                              'directory_into_descendant',
                              'closed',
                              'concurrent_close',
                              'active_lease',
                              'copied_wrapper_double_close',
                              'owned_api_reuse',
                              'two_handle_acquisition',
                              'capability_ENOSYS',
                              'capability_ENOTSUP',
                              'capability_EOPNOTSUPP',
                              'context_EINVAL',
                              'EXDEV',
                              'acquisition_failure_cleanup',
                              'acquisition_capabilities',
                              'acquisition_identity_swap',
                              'close_error_no_retry',
                              'fd_zero',
                              'no_unsafe_fallback',
                              'private_api']}
COUNTS = {'TestPrivateCreateOpenBoundary': 12, 'TestAuthorityBoundary': 12, 'TestLinuxAuthority': 8, 'TestNativeHandleBoundary': 40}
NESTED = {'TestPrivateCreateOpenBoundary': ['ExclusiveFileModeAfterSecurityBinding/security',
                                   'ExclusiveFileModeAfterSecurityBinding/owner',
                                   'ExclusiveFileModeAfterSecurityBinding/hardlink',
                                   'ExclusiveFileModeAfterSecurityBinding/excess-mode',
                                   'ExclusiveFileModeAfterSecurityBinding/security-read',
                                   'ExistingObjectNeverRepairedTruncatedRemoved/file',
                                   'ExistingObjectNeverRepairedTruncatedRemoved/directory',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused/symlink',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused/directory',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused/fifo',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused/mode',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused/special',
                                   'ExistingSymlinkWrongTypeOwnerModeSpecialRefused/owner-injected',
                                   'PostCreateReplacementRefused/directory-repin',
                                   'PostCreateReplacementRefused/file-before-bind',
                                   'PostCreateReplacementRefused/file-after-chmod',
                                   'PostCreateReplacementRefused/parent-repin',
                                   'RestrictiveUmaskDirectoryRetained/000',
                                   'RestrictiveUmaskDirectoryRetained/077',
                                   'RestrictiveUmaskDirectoryRetained/200',
                                   'RestrictiveUmaskDirectoryRetained/700',
                                   'RestrictiveUmaskDirectoryRetained/777',
                                   'ExactRecoveryPaths/ParentAuthorityFailure'],
 'TestAuthorityBoundary': ['SecurityDriftSameInode/RepositoryLeafBetweenObservations',
                           'SecurityDriftSameInode/AncestorBetweenObservations'],
 'TestLinuxAuthority': [],
 'TestNativeHandleBoundary': []}
MODES = {'private-focused': 'TestPrivateCreateOpenBoundary', 'authority-common': 'TestAuthorityBoundary', 'authority-linux': 'TestLinuxAuthority',
         'native-focused': 'TestNativeHandleBoundary'}
# Exact Linux-supported private nodes, read from the bound private test sources.
SUPPORT = {'TestPrivateNativeACLRefusal': ['File',
                                 'Directory',
                                 'ExclusiveCreatedFileBeforeChmod'],
 'TestPrivateDirectoryRefusals': ['symlink',
                                  'file',
                                  'mode',
                                  'special',
                                  'owner-injected'],
 'TestPrivateExistingOpenSwap': [],
 'TestPrivateFilePostChmodDrift': ['gid-injected',
                                   'owner-injected',
                                   'security-injected',
                                   'mount-injected',
                                   'nlink',
                                   'mode',
                                   'parent-mode'],
 'TestPrivateHandleOwnership': ['IndependentFileDescriptions',
                                'IndependentDirectoryAncestry',
                                'FailureReleasesParentLease'],
 'TestPrivateClosedAndInvalid': [],
 'TestPrivateFileCreateTransitionNative': [],
 'TestPrivateFileCreateTransitionComparison': ['NonDirectoryParent',
                                               'parent/identity-valid',
                                               'parent/identity-device',
                                               'parent/identity-inode',
                                               'parent/identity-kind',
                                               'parent/uid',
                                               'parent/gid',
                                               'parent/mode',
                                               'parent/filesystem-state',
                                               'parent/filesystem-model',
                                               'parent/filesystem-kind',
                                               'parent/filesystem-id',
                                               'parent/mount-flags',
                                               'parent/security-state',
                                               'parent/security-mechanism',
                                               'higher-ancestor/identity-valid',
                                               'higher-ancestor/identity-device',
                                               'higher-ancestor/identity-inode',
                                               'higher-ancestor/identity-kind',
                                               'higher-ancestor/uid',
                                               'higher-ancestor/gid',
                                               'higher-ancestor/mode',
                                               'higher-ancestor/filesystem-state',
                                               'higher-ancestor/filesystem-model',
                                               'higher-ancestor/filesystem-kind',
                                               'higher-ancestor/filesystem-id',
                                               'higher-ancestor/mount-flags',
                                               'higher-ancestor/security-state',
                                               'higher-ancestor/security-mechanism',
                                               'higher-ancestor/nlink'],
 'TestPrivateFileCreateTransitionGuards': ['nonregular-fd',
                                           'name-after-parent',
                                           'parent-during-observation'],
 'TestPrivateFileCreateTransitionPhases': ['precreation',
                                           'postcreation-unstable',
                                           'later-prechmod',
                                           'later-postchmod',
                                           'open',
                                           'failedcreate'],
 'TestPrivateFileCloseLease': [],
 'TestPrivateMkdirTransition': ['NativeParentCounts',
                                'identity',
                                'uid',
                                'gid',
                                'mode',
                                'security',
                                'filesystem',
                                'mount',
                                'higher-nlink',
                                'NarrowScope/precreation',
                                'NarrowScope/later',
                                'NarrowScope/open',
                                'NarrowScope/filecreate',
                                'NarrowScope/failedmkdir'],
 'TestPrivatePhysicalBoundary': [],
 'TestPrivateUmaskProcess': []}

# Source-bound 31c inventory; slash-containing t.Run names do not imply intermediate nodes.
INVENTORIES = {'TestFlockBoundary': ['FileLockSerializesProcesses',
 'DirectoryLockSerializesProcesses',
 'IndependentDescriptionPerLease',
 'ContentionCancellation',
 'EINTRRetryAndCancellation',
 'CloseCancelsWaiters',
 'ReturnedLeasePinsAncestry',
 'ConcurrentReleaseOnce',
 'UnlockCloseErrorsJoined',
 'FailedAcquisitionReleasesResources',
 'PreWaitPolicyDrift',
 'PostWaitPolicyIdentityTopologyDrift',
 'UnsupportedFlockRefused',
 'NoSystemAncestorFlock',
 'NoC4Classification'], **INVENTORIES}
COUNTS = {'TestFlockBoundary': 15, **COUNTS}
NESTED['TestFlockBoundary'] = ['IndependentDescriptionPerLease/file',
 'IndependentDescriptionPerLease/directory',
 'ContentionCancellation/file',
 'ContentionCancellation/file/cancel',
 'ContentionCancellation/file/deadline',
 'ContentionCancellation/directory',
 'ContentionCancellation/directory/cancel',
 'ContentionCancellation/directory/deadline',
 'EINTRRetryAndCancellation/file',
 'EINTRRetryAndCancellation/file/retry',
 'EINTRRetryAndCancellation/file/cancel',
 'EINTRRetryAndCancellation/directory',
 'EINTRRetryAndCancellation/directory/retry',
 'EINTRRetryAndCancellation/directory/cancel',
 'CloseCancelsWaiters/file',
 'CloseCancelsWaiters/file/waiter',
 'CloseCancelsWaiters/file/publication-loses',
 'CloseCancelsWaiters/directory',
 'CloseCancelsWaiters/directory/waiter',
 'CloseCancelsWaiters/directory/publication-loses',
 'ReturnedLeasePinsAncestry/file',
 'ReturnedLeasePinsAncestry/directory',
 'ConcurrentReleaseOnce/file',
 'ConcurrentReleaseOnce/directory',
 'UnlockCloseErrorsJoined/file',
 'UnlockCloseErrorsJoined/directory',
 'FailedAcquisitionReleasesResources/file',
 'FailedAcquisitionReleasesResources/file/open',
 'FailedAcquisitionReleasesResources/file/wait',
 'FailedAcquisitionReleasesResources/file/postguard',
 'FailedAcquisitionReleasesResources/file/publication',
 'FailedAcquisitionReleasesResources/file/publication-joined-cleanup',
 'FailedAcquisitionReleasesResources/directory',
 'FailedAcquisitionReleasesResources/directory/open',
 'FailedAcquisitionReleasesResources/directory/wait',
 'FailedAcquisitionReleasesResources/directory/postguard',
 'FailedAcquisitionReleasesResources/directory/publication',
 'FailedAcquisitionReleasesResources/directory/publication-joined-cleanup',
 'PreWaitPolicyDrift/file',
 'PreWaitPolicyDrift/file/parent/uid',
 'PreWaitPolicyDrift/file/parent/gid',
 'PreWaitPolicyDrift/file/parent/mode',
 'PreWaitPolicyDrift/file/parent/acl',
 'PreWaitPolicyDrift/file/parent/mount',
 'PreWaitPolicyDrift/file/parent/nlink',
 'PreWaitPolicyDrift/file/leaf/uid',
 'PreWaitPolicyDrift/file/leaf/gid',
 'PreWaitPolicyDrift/file/leaf/mode',
 'PreWaitPolicyDrift/file/leaf/acl',
 'PreWaitPolicyDrift/file/leaf/mount',
 'PreWaitPolicyDrift/file/leaf/nlink',
 'PreWaitPolicyDrift/directory',
 'PreWaitPolicyDrift/directory/parent/uid',
 'PreWaitPolicyDrift/directory/parent/gid',
 'PreWaitPolicyDrift/directory/parent/mode',
 'PreWaitPolicyDrift/directory/parent/acl',
 'PreWaitPolicyDrift/directory/parent/mount',
 'PreWaitPolicyDrift/directory/parent/nlink',
 'PreWaitPolicyDrift/directory/leaf/uid',
 'PreWaitPolicyDrift/directory/leaf/gid',
 'PreWaitPolicyDrift/directory/leaf/mode',
 'PreWaitPolicyDrift/directory/leaf/acl',
 'PreWaitPolicyDrift/directory/leaf/mount',
 'PreWaitPolicyDrift/directory/leaf/nlink',
 'PostWaitPolicyIdentityTopologyDrift/file',
 'PostWaitPolicyIdentityTopologyDrift/file/parent/uid',
 'PostWaitPolicyIdentityTopologyDrift/file/parent/gid',
 'PostWaitPolicyIdentityTopologyDrift/file/parent/mode',
 'PostWaitPolicyIdentityTopologyDrift/file/parent/acl',
 'PostWaitPolicyIdentityTopologyDrift/file/parent/mount',
 'PostWaitPolicyIdentityTopologyDrift/file/parent/nlink',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf/uid',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf/gid',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf/mode',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf/acl',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf/mount',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf/nlink',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf-replace',
 'PostWaitPolicyIdentityTopologyDrift/file/leaf-symlink',
 'PostWaitPolicyIdentityTopologyDrift/file/ancestor-replace',
 'PostWaitPolicyIdentityTopologyDrift/file/ancestor-symlink',
 'PostWaitPolicyIdentityTopologyDrift/file/above-boundary',
 'PostWaitPolicyIdentityTopologyDrift/directory',
 'PostWaitPolicyIdentityTopologyDrift/directory/parent/uid',
 'PostWaitPolicyIdentityTopologyDrift/directory/parent/gid',
 'PostWaitPolicyIdentityTopologyDrift/directory/parent/mode',
 'PostWaitPolicyIdentityTopologyDrift/directory/parent/acl',
 'PostWaitPolicyIdentityTopologyDrift/directory/parent/mount',
 'PostWaitPolicyIdentityTopologyDrift/directory/parent/nlink',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf/uid',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf/gid',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf/mode',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf/acl',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf/mount',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf/nlink',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf-replace',
 'PostWaitPolicyIdentityTopologyDrift/directory/leaf-symlink',
 'PostWaitPolicyIdentityTopologyDrift/directory/ancestor-replace',
 'PostWaitPolicyIdentityTopologyDrift/directory/ancestor-symlink',
 'PostWaitPolicyIdentityTopologyDrift/directory/above-boundary',
 'UnsupportedFlockRefused/file',
 'UnsupportedFlockRefused/file/ENOSYS',
 'UnsupportedFlockRefused/file/ENOTSUP',
 'UnsupportedFlockRefused/file/EOPNOTSUPP',
 'UnsupportedFlockRefused/file/EINVAL',
 'UnsupportedFlockRefused/file/EBADF',
 'UnsupportedFlockRefused/directory',
 'UnsupportedFlockRefused/directory/ENOSYS',
 'UnsupportedFlockRefused/directory/ENOTSUP',
 'UnsupportedFlockRefused/directory/EOPNOTSUPP',
 'UnsupportedFlockRefused/directory/EINVAL',
 'UnsupportedFlockRefused/directory/EBADF',
 'NoSystemAncestorFlock/root',
 'NoSystemAncestorFlock/temporary-root',
 'NoSystemAncestorFlock/file',
 'NoSystemAncestorFlock/directory',
 'NoC4Classification/file',
 'NoC4Classification/directory']
MODES = {'flock-focused': 'TestFlockBoundary', **MODES}
FLOCK_SUPPORT = {'TestFlockFinalObservationCancellation': ['file', 'directory'],
 'TestFlockInputsAndPublication': ['file',
                                   'file/nil-context',
                                   'file/canceled',
                                   'file/nil-dir',
                                   'file/zero-dir',
                                   'file/closed-dir',
                                   'file/cancel-after-publication',
                                   'directory',
                                   'directory/nil-context',
                                   'directory/canceled',
                                   'directory/nil-dir',
                                   'directory/zero-dir',
                                   'directory/closed-dir',
                                   'directory/cancel-after-publication'],
 'TestFlockProcess': []}
FLOCK_CHILDREN = {'FileLockSerializesProcesses': 'false', 'DirectoryLockSerializesProcesses': 'true', 'ReturnedLeasePinsAncestry/file': 'false', 'ReturnedLeasePinsAncestry/directory': 'true'}

# Source-bound coordinator32 inventory; no implicit intermediate slash nodes.
INVENTORIES = {'TestCoordinatorBoundary': ['CanonicalEUIDIgnoresEnvironment', 'UserBeforeResolution', 'OneCompleteBatch', 'AliasDedupStableOrder', 'AliasRetargetRefused', 'PhysicalAuthorityDrift', 'ExplicitEEXISTOpen', 'CreationRecordsSurviveFailure', 'CancellationUnwindsReverse', 'ReleaseConcurrentOnceAllErrors', 'NoSharedDescription', 'NoAnchorRemoval', 'BootstrapIdentityContinuity', 'MissingRepositoryRefused'], **INVENTORIES}
COUNTS = {'TestCoordinatorBoundary': 14, **COUNTS}
NESTED['TestCoordinatorBoundary'] = ['CanonicalEUIDIgnoresEnvironment/environment-',
 'CanonicalEUIDIgnoresEnvironment/environment-/not/a/repository',
 'CanonicalEUIDIgnoresEnvironment/environment-relative',
 'CanonicalEUIDIgnoresEnvironment/authority-before-create',
 'OneCompleteBatch/false',
 'OneCompleteBatch/true',
 'OneCompleteBatch/concurrent-winner-and-release',
 'AliasDedupStableOrder/conflicting-physical-topologies',
 'PhysicalAuthorityDrift/earlier-repository',
 'PhysicalAuthorityDrift/repository-ancestor',
 'PhysicalAuthorityDrift/final-user-file',
 'PhysicalAuthorityDrift/final-anchor',
 'PhysicalAuthorityDrift/final-root',
 'PhysicalAuthorityDrift/empty-anchor',
 'ExplicitEEXISTOpen/exact',
 'ExplicitEEXISTOpen/joined',
 'ExplicitEEXISTOpen/wrapped',
 'ExplicitEEXISTOpen/nested-errno',
 'ExplicitEEXISTOpen/path',
 'ExplicitEEXISTOpen/kind',
 'ExplicitEEXISTOpen/observations',
 'ExplicitEEXISTOpen/created',
 'ExplicitEEXISTOpen/wrong-op',
 'ExplicitEEXISTOpen/wrong-path',
 'ExplicitEEXISTOpen/disappearance-false',
 'ExplicitEEXISTOpen/disappearance-true',
 'CreationRecordsSurviveFailure/file-create',
 'CreationRecordsSurviveFailure/flock',
 'CreationRecordsSurviveFailure/batch',
 'CancellationUnwindsReverse/first-authority-release-false',
 'CancellationUnwindsReverse/first-authority-release-true',
 'BootstrapIdentityContinuity/replacement',
 'BootstrapIdentityContinuity/close-failure',
 'MissingRepositoryRefused/missing',
 'MissingRepositoryRefused/dangling',
 'MissingRepositoryRefused/file',
 'MissingRepositoryRefused/relative',
 'MissingRepositoryRefused/nul']
MODES = {'coordinator-focused': 'TestCoordinatorBoundary', **MODES}
COORDINATOR_SUPPORT = {'TestCoordinatorProcess': []}

# Source-bound regular33a names, including four actual intermediate nodes.
INVENTORIES = {'TestRegularObservationBoundary': ['EmptyBinaryAndStreamingDigest',
 'MetadataNormalizationAndExactEquality',
 'GeometryOnlyPayloadPolicy',
 'InvalidComponentMissingAndWrongKind',
 'SpecialEntryAndSymlinkSwapRefusal',
 'PreOpenFDNameBinding',
 'FullAncestryAndLeafReplacement',
 'SameInodeContentAndMetadataDrift',
 'ShrinkGrowthAndEOFProbe',
 'ShortReadEINTRAndReadFailures',
 'StatGuardAndOpenFailures',
 'ContextAndParentCloseCheckpoints',
 'CopiedParentConcurrentObservationAndPublication',
 'CloseOnceJoinedErrorsAndZeroResults',
 'AtimeExclusionImmutableAPIAndUnavailableTargets'], **INVENTORIES}
COUNTS = {'TestRegularObservationBoundary': 15, **COUNTS}
NESTED['TestRegularObservationBoundary'] = ['EmptyBinaryAndStreamingDigest/empty',
 'EmptyBinaryAndStreamingDigest/binary',
 'EmptyBinaryAndStreamingDigest/streaming',
 'MetadataNormalizationAndExactEquality/exact_fields',
 'MetadataNormalizationAndExactEquality/malformed',
 'MetadataNormalizationAndExactEquality/native_normalization',
 'GeometryOnlyPayloadPolicy/native_modes_hardlinks',
 'GeometryOnlyPayloadPolicy/counterfactual_foreign_uid_special_bits',
 'InvalidComponentMissingAndWrongKind/component',
 'InvalidComponentMissingAndWrongKind/missing',
 'InvalidComponentMissingAndWrongKind/directory',
 'InvalidComponentMissingAndWrongKind/symlink',
 'InvalidComponentMissingAndWrongKind/fifo',
 'InvalidComponentMissingAndWrongKind/nil_parent',
 'InvalidComponentMissingAndWrongKind/zero_parent',
 'InvalidComponentMissingAndWrongKind/closed_parent',
 'SpecialEntryAndSymlinkSwapRefusal/symlink_before_open',
 'SpecialEntryAndSymlinkSwapRefusal/fifo_before_open',
 'SpecialEntryAndSymlinkSwapRefusal/counterfactual_device_fd',
 'PreOpenFDNameBinding/opened_fd_replacement',
 'PreOpenFDNameBinding/name_after_open',
 'FullAncestryAndLeafReplacement/ancestor_before_open',
 'FullAncestryAndLeafReplacement/ancestor_after_read',
 'FullAncestryAndLeafReplacement/parent_after_read',
 'FullAncestryAndLeafReplacement/leaf_after_read',
 'FullAncestryAndLeafReplacement/root_edge_injection',
 'FullAncestryAndLeafReplacement/every_ancestry_edge',
 'SameInodeContentAndMetadataDrift/native_content',
 'SameInodeContentAndMetadataDrift/native_mode',
 'SameInodeContentAndMetadataDrift/native_nlink',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields',
 'ShrinkGrowthAndEOFProbe/native_shrink',
 'ShrinkGrowthAndEOFProbe/native_growth',
 'ShrinkGrowthAndEOFProbe/empty_probe_growth',
 'ShrinkGrowthAndEOFProbe/maximum_signed_size',
 'ShrinkGrowthAndEOFProbe/exact_probe',
 'ShortReadEINTRAndReadFailures/short_reads',
 'ShortReadEINTRAndReadFailures/eintr',
 'ShortReadEINTRAndReadFailures/eintr_after_progress',
 'ShortReadEINTRAndReadFailures/probe_eintr',
 'ShortReadEINTRAndReadFailures/read_eio',
 'ShortReadEINTRAndReadFailures/probe_eio',
 'ShortReadEINTRAndReadFailures/negative_count',
 'ShortReadEINTRAndReadFailures/oversized_count',
 'ShortReadEINTRAndReadFailures/eintr_positive_count',
 'ShortReadEINTRAndReadFailures/error_positive_count',
 'ShortReadEINTRAndReadFailures/repeated_eintr_cancel',
 'StatGuardAndOpenFailures/phases',
 'StatGuardAndOpenFailures/native_open_causes',
 'StatGuardAndOpenFailures/invalid_open_count',
 'ContextAndParentCloseCheckpoints/nil_context',
 'ContextAndParentCloseCheckpoints/pre_cancelled',
 'ContextAndParentCloseCheckpoints/deadline',
 'ContextAndParentCloseCheckpoints/guard_cancel',
 'ContextAndParentCloseCheckpoints/guard_parent_close',
 'ContextAndParentCloseCheckpoints/baseline_cancel',
 'ContextAndParentCloseCheckpoints/baseline_parent_close',
 'ContextAndParentCloseCheckpoints/open_cancel',
 'ContextAndParentCloseCheckpoints/open_parent_close',
 'ContextAndParentCloseCheckpoints/fd_cancel',
 'ContextAndParentCloseCheckpoints/fd_parent_close',
 'ContextAndParentCloseCheckpoints/name_cancel',
 'ContextAndParentCloseCheckpoints/name_parent_close',
 'ContextAndParentCloseCheckpoints/read_cancel',
 'ContextAndParentCloseCheckpoints/read_parent_close',
 'ContextAndParentCloseCheckpoints/probe_cancel',
 'ContextAndParentCloseCheckpoints/probe_parent_close',
 'ContextAndParentCloseCheckpoints/final_stat_cancel',
 'ContextAndParentCloseCheckpoints/final_stat_parent_close',
 'ContextAndParentCloseCheckpoints/final_guard_cancel',
 'ContextAndParentCloseCheckpoints/final_guard_parent_close',
 'ContextAndParentCloseCheckpoints/close_cancel',
 'ContextAndParentCloseCheckpoints/close_parent_close',
 'CopiedParentConcurrentObservationAndPublication/independent_offsets',
 'CopiedParentConcurrentObservationAndPublication/copied_parent_close',
 'CopiedParentConcurrentObservationAndPublication/close_during_cleanup',
 'CopiedParentConcurrentObservationAndPublication/publication_mutex',
 'CopiedParentConcurrentObservationAndPublication/publication_wins',
 'CloseOnceJoinedErrorsAndZeroResults/close_failure',
 'CloseOnceJoinedErrorsAndZeroResults/joined_read_close',
 'CloseOnceJoinedErrorsAndZeroResults/joined_context_close',
 'CloseOnceJoinedErrorsAndZeroResults/fd_zero',
 'CloseOnceJoinedErrorsAndZeroResults/open_failure_no_close',
 'CloseOnceJoinedErrorsAndZeroResults/reused_number_not_reclosed',
 'AtimeExclusionImmutableAPIAndUnavailableTargets/atime_excluded',
 'AtimeExclusionImmutableAPIAndUnavailableTargets/immutable_values',
 'AtimeExclusionImmutableAPIAndUnavailableTargets/exact_api',
 'AtimeExclusionImmutableAPIAndUnavailableTargets/unavailable_compile_contract',
 'MetadataNormalizationAndExactEquality/exact_fields/device',
 'MetadataNormalizationAndExactEquality/exact_fields/inode',
 'MetadataNormalizationAndExactEquality/exact_fields/kind',
 'MetadataNormalizationAndExactEquality/exact_fields/identity_valid',
 'MetadataNormalizationAndExactEquality/exact_fields/uid',
 'MetadataNormalizationAndExactEquality/exact_fields/gid',
 'MetadataNormalizationAndExactEquality/exact_fields/mode',
 'MetadataNormalizationAndExactEquality/exact_fields/nlink',
 'MetadataNormalizationAndExactEquality/exact_fields/size',
 'MetadataNormalizationAndExactEquality/exact_fields/mtime_sec',
 'MetadataNormalizationAndExactEquality/exact_fields/mtime_nsec',
 'MetadataNormalizationAndExactEquality/exact_fields/ctime_sec',
 'MetadataNormalizationAndExactEquality/exact_fields/ctime_nsec',
 'MetadataNormalizationAndExactEquality/malformed/identity',
 'MetadataNormalizationAndExactEquality/malformed/kind',
 'MetadataNormalizationAndExactEquality/malformed/mode',
 'MetadataNormalizationAndExactEquality/malformed/size',
 'MetadataNormalizationAndExactEquality/malformed/mtime_negative',
 'MetadataNormalizationAndExactEquality/malformed/mtime_billion',
 'MetadataNormalizationAndExactEquality/malformed/ctime_negative',
 'MetadataNormalizationAndExactEquality/malformed/ctime_billion',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/uid',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/gid',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/mode',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/nlink',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/size',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/mtime_sec',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/mtime_nsec',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/ctime_sec',
 'SameInodeContentAndMetadataDrift/counterfactual_all_fields/ctime_nsec',
 'StatGuardAndOpenFailures/phases/guard_fd',
 'StatGuardAndOpenFailures/phases/guard_edge',
 'StatGuardAndOpenFailures/phases/baseline',
 'StatGuardAndOpenFailures/phases/open',
 'StatGuardAndOpenFailures/phases/fd_before_read',
 'StatGuardAndOpenFailures/phases/name_before_read',
 'StatGuardAndOpenFailures/phases/fd_after_read',
 'StatGuardAndOpenFailures/phases/name_after_read',
 'StatGuardAndOpenFailures/phases/final_guard',
 'StatGuardAndOpenFailures/phases/malformed_baseline',
 'StatGuardAndOpenFailures/phases/malformed_fd']
MODES = {'regular-focused': 'TestRegularObservationBoundary', **MODES}

UMASKS = {'000': '0700', '077': '0700', '200': '0500', '700': '0000', '777': '0000'}

PACKAGE = 'github.com/lmilojevicc/dotty/internal/nativefs '


# Independent source-index inventory; these are canonical nodes, not count acceptance.
PROTOCOL_TESTS = {'TestFocusedProtocolRecordFraming': ['exact',
                                      'empty',
                                      'partial',
                                      'malformed',
                                      'stale',
                                      'wrong_phase',
                                      'wrong_epoch',
                                      'wrong_anchor',
                                      'wrong_action',
                                      'newline',
                                      'newlines',
                                      'trailing_bytes',
                                      'NUL_suffix',
                                      'NUL_prefix',
                                      'NUL_middle'],
 'TestFocusedProtocolPublication': ['initial_absence_boundary',
                                    'initial_absence_boundary/initial_absence_is_pending',
                                    'initial_absence_boundary/wrapped_or_joined_absence_is_fatal',
                                    'initial_absence_boundary/observed_disappearance_is_fatal'],
 'TestFocusedProtocolWakeClassification': ['benign_empty_wake',
                                           'old_timeout_rejected',
                                           'interruption',
                                           'TERM_interruption',
                                           'data',
                                           'EOF',
                                           'unexplained',
                                           'unobserved_interruption',
                                           'partial_data',
                                           'finished_interruption_is_observed_but_ignored',
                                           'actual_pipe_EOF_latches'],
 'TestFocusedProtocolDescriptorClosure': [],
 'TestFocusedProtocolSolePulseWriter': [],
 'TestFocusedProtocolPulseWake': [],
 'TestFocusedProtocolNoPulseNegative': [],
 'TestFocusedProtocolPulseCancellation': ['before_anchor/interrupt',
                                          'before_anchor/terminated',
                                          'transition/interrupt',
                                          'transition/terminated'],
 'TestFocusedProtocolParentAbort': [],
 'TestFocusedProtocolOwnerOnce': ['false', 'true'],
 'TestFocusedProtocolBoundedCapture': ['limit_minus_one',
                                       'limit',
                                       'limit_plus_one',
                                       'cumulative_exact',
                                       'cumulative_overflow',
                                       'empty_at_capacity'],
 'TestFocusedProtocolCaptureEOF': ['registered_cleanup_retains_incomplete_capture'],
 'TestFocusedProtocolEventTermination': ['closed_gate_EOF_before_Wait',
                                         'nongated_completion',
                                         'startup_trap_without_return',
                                         'open_gate_without_completion'],
 'TestFocusedProtocolEventByteBudget': ['262143', '262144', '262145'],
 'TestFocusedProtocolPendingContinuation': ['queued_wakes_hold',
                                            'queued_barrier_abort',
                                            'grant_during_later_pending_read',
                                            'interruption_skips_grant'],
 'TestFocusedProtocolPulseLifecycle': ['sole_LF_bytes',
                                       'queued_bytes_are_not_grants',
                                       'budget',
                                       'deadline',
                                       'backpressure',
                                       'EPIPE_before_close_event',
                                       'validated_close',
                                       'cancel_join',
                                       'cancel_blocked_write_joins'],
 'TestFocusedProtocolIdentityRecords': ['exact',
                                        'FIFO',
                                        'directory',
                                        'symlink',
                                        'replacement',
                                        'oversized',
                                        'malformed',
                                        'NUL',
                                        'trailing_newline'],
 'TestFocusedProtocolOuterIdentity': ['startup',
                                      'runner',
                                      'descendant',
                                      'wrong_group',
                                      'publication_failure_prevents_spawn',
                                      'spawn-intent_failure_prevents_spawn'],
 'TestFocusedProtocolAuthorizationHealth': ['malformed_after_cancellation',
                                            'overflow_after_cancellation',
                                            'lone_return',
                                            'wrong_anchor',
                                            'wrong_sequence',
                                            'wrong_action',
                                            'pulse_failure',
                                            'EOF_after_cancellation',
                                            'EOF_with_outstanding_entry',
                                            'empty_channel_loss'],
 'TestFocusedProtocolNegativeRetention': ['record_rejection',
                                          'record_rejection/registered_cleanup',
                                          'read_failure',
                                          'read_failure/registered_cleanup',
                                          'EOF',
                                          'EOF/registered_cleanup',
                                          'abort',
                                          'abort/registered_cleanup',
                                          'positive_removal',
                                          'positive_removal/registered_cleanup'],
 'TestFocusedProtocolFatalOwner': [],
 'TestFocusedProtocolStartFailure': [],
 'TestFocusedProtocolFatalOwnerFixture': [],
 'TestFocusedProtocolCleanupUncertainty': ['complete_pre-start_witness',
                                           'complete_pre-start_witness/partial_ACK_without_second_publication',
                                           'complete_pre-start_witness/short_ACK_without_second_publication',
                                           'complete_pre-start_witness/missing_witness',
                                           'complete_pre-start_witness/malformed_witness',
                                           'complete_pre-start_witness/partial_witness',
                                           'complete_pre-start_witness/NUL_witness',
                                           'complete_pre-start_witness/oversized_witness',
                                           'complete_pre-start_witness/competing_return_record',
                                           'complete_pre-start_witness/contradictory_return_record',
                                           'complete_pre-start_witness/child_start',
                                           'complete_pre-start_witness/spawn_intent',
                                           'complete_pre-start_witness/spawn_completion',
                                           'complete_pre-start_witness/descendant_start',
                                           'complete_pre-start_witness/descendant_readiness',
                                           'complete_pre-start_witness/FIFO_witness',
                                           'complete_pre-start_witness/directory_witness',
                                           'complete_pre-start_witness/symlink_witness'],
 'TestFocusedProtocolEventChannel': ['complete',
                                     'partial',
                                     'NUL',
                                     'wrong_epoch',
                                     'oversized',
                                     'overflow',
                                     'duplicate',
                                     'fragmented_complete_event',
                                     'read_error_latches',
                                     'open_writer_is_incomplete',
                                     'anchor_inheritance',
                                     'Go_fixture_boundary_does_not_start_pulses']}
PROTOCOL_MATRIX = {'TestFocusedTaskProtocol': ['success', 'exit7', 'build_failure', 'startup_failure', 'signal_status', 'before_anchor_INT', 'before_anchor_TERM', 'build_readiness_INT', 'build_readiness_TERM', 'transition_INT', 'transition_TERM', 'startup_INT', 'startup_TERM', 'partial_readiness', 'truncated_readiness', 'invalid_readiness', 'truncated_readiness_INT', 'invalid_readiness_TERM', 'unterminated_readiness_INT', 'unterminated_readiness_TERM', 'ack_transition_INT', 'ack_transition_TERM', 'ack_write_INT', 'ack_write_TERM', 'ack_write_failure', 'delivered_ack_failure', 'invalid_ack', 'ack_EOF', 'partial_ack_EOF', 'partial_ack_open_INT', 'partial_ack_open_TERM', 'short_invalid_ack_open_INT', 'short_invalid_ack_open_TERM', 'early_runner_exit', 'partial_readiness_INT', 'partial_readiness_TERM', 'completion_INT', 'completion_TERM', 'finished_INT', 'finished_TERM', 'interrupted_waits_INT', 'interrupted_waits_TERM']}
PROTOCOL_CANCELLATION = {'TestFocusedCancellation': ['runner/retained_pipe/interrupt', 'runner/retained_pipe/terminated', 'wrapper_runner/retained_pipe/interrupt', 'wrapper_runner/retained_pipe/terminated', 'wrapper_build/retained_pipe/interrupt', 'wrapper_build/retained_pipe/terminated', 'runner/early_close/interrupt', 'runner/early_close/terminated', 'wrapper_runner/early_close/interrupt', 'wrapper_runner/early_close/terminated', 'runner/driver_exits/interrupt', 'runner/driver_exits/terminated', 'wrapper_runner/driver_exits/interrupt', 'wrapper_runner/driver_exits/terminated']}
PROTOCOL_CHILD = {'TestFocusedChildInvocationContext': [], 'TestFocusedChildStartErrors': ['existing_command_error', 'missing', 'not_executable'], 'TestFocusedChildSignalExit': []}
PROTOCOL_ORDER = ['RecordFraming', 'Publication', 'IdentityRecords', 'OuterIdentity', 'AuthorizationHealth', 'WakeClassification', 'BoundedCapture', 'OwnerOnce', 'NegativeRetention', 'StartFailure', 'FatalOwner', 'ParentAbort', 'CleanupUncertainty', 'CaptureEOF', 'EventChannel', 'EventTermination', 'EventByteBudget', 'DescriptorClosure', 'SolePulseWriter', 'PulseLifecycle', 'NoPulseNegative', 'PulseWake', 'PendingContinuation', 'PulseCancellation']
PROTOCOL_LANES = ["protocol-" + x for x in PROTOCOL_ORDER] + ["protocol-matrix", "protocol-cancellation", "protocol-child"]

PROTOCOL_ROOT = None
PROTOCOL_CALLER = r'task_unix_test\.go:[0-9]+'


def fatal_child_lines(text, required):
    """Return only the two validated negative lines; nothing else is exempted."""
    lines = text.splitlines()
    parent = 'TestFocusedProtocolFatalOwner'
    starts = [i for i, l in enumerate(lines) if re.fullmatch(r'=== RUN\s+' + parent, l)]
    ends = [i for i, l in enumerate(lines) if re.fullmatch(r'--- PASS: ' + parent + r' \([0-9.]+s\)', l)]
    failures = [i for i, l in enumerate(lines) if re.fullmatch(r'[ \t]+--- FAIL: TestFocusedProtocolFatalOwnerFixture \([0-9.]+s\)', l)]
    if not required:
        assert not failures, 'unexpected fatal helper failure'
        return set()
    assert len(starts) == len(ends) == len(failures) == 1, 'fatal parent/child singleton'
    start, end, failure = starts[0], ends[0], failures[0]
    assert start < failure < end, 'fatal child outside active parent'
    assert not any(re.match(r'^[ \t]*(?:=== RUN|--- (?:PASS|FAIL|SKIP):)', l)
                   and i != failure and not re.fullmatch(r'[ \t]+=== RUN\s+TestFocusedProtocolFatalOwnerFixture', l)
                   for i,l in enumerate(lines[start+1:end],start+1)), 'fatal foreign canonical event'
    block = lines[start+1:end]
    declarations = [re.fullmatch(r'    ' + PROTOCOL_CALLER + r': private protocol evidence: (' + PROTOCOL_ROOT + r')', l) for l in block]
    roots = [m[1] for m in declarations if m]
    assert len(roots) == len(set(roots)) == 2, 'fatal two exact distinct declared roots'
    outer, child = roots
    def one(pattern):
        matches = [i for i in range(start+1, end) if re.fullmatch(pattern, lines[i])]
        assert len(matches) == 1, 'fatal missing/duplicate/invalid field: ' + pattern
        return matches[0]
    suffix = r'; identities are historical observations, never later signal authority'
    outer_outcome = one(r'    ' + PROTOCOL_CALLER + ': protocol root=' + re.escape(outer) + r' phase="" epoch= outcome=Wait result=exit status 1 cleanup-complete=true retained=true' + suffix)
    header = one(r'    ' + PROTOCOL_CALLER + ': preserved final stdout/stderr root=' + re.escape(outer) + r' truncated=false:')
    marker = one(r'            ' + PROTOCOL_CALLER + r': intentional private fatal owner unwind')
    nested = one(r'            ' + PROTOCOL_CALLER + ': protocol root=' + re.escape(child) + r' phase="reader" epoch=' + child.rsplit('/',1)[1] + r' outcome=Wait result=signal: terminated cleanup-complete=true retained=true' + suffix)
    nested_header = one(r'            ' + PROTOCOL_CALLER + ': preserved final stdout/stderr root=' + re.escape(child) + r' truncated=false:')
    nested_retained = one(r'            ' + PROTOCOL_CALLER + ': retained protocol evidence ' + re.escape(child) + r'; output-complete=true')
    terminal = one(r'        FAIL')
    trailer = one(r'    ' + PROTOCOL_CALLER + ': retained protocol evidence ' + re.escape(outer) + r'; output-complete=true')
    declaration_positions = [start+1+i for i,m in enumerate(declarations) if m]
    assert max(declaration_positions) < outer_outcome < header < failure < marker < nested < nested_header < nested_retained < terminal < trailer, 'fatal transcript ordering'
    child_runs = [i for i,l in enumerate(lines) if re.fullmatch(r'[ \t]+=== RUN\s+TestFocusedProtocolFatalOwnerFixture',l)]
    assert not child_runs or len(child_runs) == 1 and header < child_runs[0] < failure, 'fatal child RUN placement'
    trace = one(r'            ' + PROTOCOL_CALLER + r': wrapper-trace \(final bounded tail\):')
    assert nested_retained < trace < terminal, 'fatal trace placement'
    expected_lines = {failure, marker, nested, nested_header, nested_retained, trace, *child_runs}
    for i in range(header+1,terminal):
        if i in expected_lines: continue
        assert trace < i < terminal and (lines[i].startswith('                ') or not lines[i].strip()), 'unexpected fatal child diagnostic'
    assert not re.search(r'truncated=true|\[compiler output truncated:', '\n'.join(block)), 'fatal truncated capture'
    return {failure, terminal}


def reject_failures(text, fatal=False):
    exempt = fatal_child_lines(text, fatal)
    for i, line in enumerate(text.splitlines()):
        if i not in exempt:
            assert not re.match(r'^[ \t]*(?:--- (?:SKIP|FAIL):|FAIL(?:\s|$)|skip(?:ped)?:)', line, re.I), 'skip/failure in output'
    assert not re.search(r'^[ \t]*\[compiler output truncated:', text, re.M), 'child capture truncated'


def protocol_inventory(mode):
    if mode == 'verify':
        return {**PROTOCOL_TESTS, **PROTOCOL_MATRIX, **PROTOCOL_CANCELLATION, **PROTOCOL_CHILD,
                'TestFocusedTaskProtocolFixture': [], 'TestFocusedCancellationProcessFixture': []}
    if mode == 'protocol-matrix': return PROTOCOL_MATRIX
    if mode == 'protocol-cancellation': return PROTOCOL_CANCELLATION
    if mode == 'protocol-child': return PROTOCOL_CHILD
    suffix = mode.removeprefix('protocol-')
    assert suffix in PROTOCOL_ORDER, 'unknown protocol lane'
    return {'TestFocusedProtocol' + suffix: PROTOCOL_TESTS['TestFocusedProtocol' + suffix]}


def check_protocol(text, mode):
    inventory = protocol_inventory(mode)
    required_fatal = 'TestFocusedProtocolFatalOwner' in inventory
    reject_failures(text, required_fatal)
    runs = Counter(re.findall(r'^=== RUN\s+(\S+)\s*$', text, re.M))
    passes = Counter(re.findall(r'^[ \t]*--- PASS: (\S+) \([^\n]*\)[ \t]*$', text, re.M))
    prefix = ('TestFocusedProtocol', 'TestFocusedTaskProtocol', 'TestFocusedCancellation', 'TestFocusedChild')
    if mode == 'verify':
        runs = Counter({k:v for k,v in runs.items() if k.startswith(prefix)})
        passes = Counter({k:v for k,v in passes.items() if k.startswith(prefix)})
    assert runs == passes, 'protocol RUN/PASS mismatch'
    for name in inventory:
        assert len(re.findall(r'^--- PASS: ' + re.escape(name) + r' \([^\n]*\)$', text, re.M)) == 1, 'protocol canonical root PASS missing'
    # The sole dynamic subcase is source-named: absolute TempDir driver path.
    dynamic = 'TestFocusedChildInvocationContext/' + re.escape(str(PRIVATE_TMP)) + r'/TestFocusedChildInvocationContext[0-9]+/001/driver_with_spaces'
    normalized = Counter()
    for name,count in runs.items():
        if re.fullmatch(dynamic,name):
            name = 'TestFocusedChildInvocationContext/' + str(PRIVATE_TMP) + '/TestFocusedChildInvocationContextROOT/001/driver_with_spaces'
        normalized[name] += count
    expected = Counter({n:1 for root,children in inventory.items() for n in [root]+[root+'/'+c for c in children]})
    assert normalized == expected, 'protocol exact source inventory mismatch'
    if mode != 'verify':
        executed = Counter(re.findall(r'^executed: (.+)$',text,re.M))
        assert executed == Counter({'github.com/lmilojevicc/dotty/internal/tools/focused '+n:v for n,v in runs.items()}), 'protocol executed mismatch'
    print('PASS exact source-index protocol inventory; helper outer PASS is inert, child expected fatal separately correlated')


def check_text(text, mode):
    if mode in PROTOCOL_LANES:
        return check_protocol(text, mode)
    assert mode in (*MODES, 'verify'), 'unknown evidence mode'
    reject_failures(text, mode == 'verify')
    if mode == 'verify':
        check_protocol(text, mode)
    all_runs = Counter(re.findall(r'^=== RUN\s+(\S+)\s*$', text, re.M))
    # Focused JSON harness emits canonical outer PASS at column zero. t.Log
    # embeds child PASS indented; those are not foreign/duplicate outer events.
    pass_indent = r'[ \t]*' if mode == 'verify' else ''
    all_passes = Counter(re.findall(r'^' + pass_indent + r'--- PASS: (\S+) \([^\n]*\)[ \t]*$', text, re.M))
    top_passes = Counter(re.findall(r'^--- PASS: ([^/\s]+) \([^\n]*\)[ \t]*$', text, re.M))
    executed = Counter(re.findall(r'^executed: (.+)$', text, re.M))
    selected = list(INVENTORIES) if mode == 'verify' else [MODES[mode]]
    for aggregator in selected:
        leaves = INVENTORIES[aggregator]
        assert len(leaves) == len(set(leaves)) == COUNTS[aggregator]
        assert executed[PACKAGE + aggregator] == 0, aggregator + ': aggregate executed marker forbidden'
        prefix = aggregator + '/'
        required = {prefix + leaf for leaf in leaves}
        allowed_nested = {prefix + leaf for leaf in NESTED[aggregator]}
        runs = Counter({k: v for k, v in all_runs.items() if k == aggregator or k.startswith(prefix)})
        passes = Counter({k: v for k, v in all_passes.items() if k == aggregator or k.startswith(prefix)})
        # Even full verbose nested PASS cannot stand in for an outer root PASS.
        # Do not fabricate a zero-count key for a missing aggregate: Python
        # <=3.9 Counter equality distinguishes it from an absent key. Indented
        # root PASS still cannot replace canonical top-level PASS.
        if top_passes[aggregator]:
            passes[aggregator] = top_passes[aggregator]
        else:
            passes.pop(aggregator, None)
        assert runs == passes, aggregator + ': RUN/PASS set/count mismatch'
        assert set(runs) <= required | allowed_nested | {aggregator}, aggregator + ': unexpected root/nested case'
        assert required | {aggregator} <= set(runs), aggregator + ': missing required case/aggregator'
        assert allowed_nested <= set(runs), 'missing required nested case: ' + aggregator
        assert all(v == 1 for v in runs.values()), aggregator + ': duplicate execution'
        roots = {k for k in runs if k.startswith(prefix) and '/' not in k[len(prefix):]}
        assert roots == required and len(roots) == COUNTS[aggregator], aggregator + ': root count mismatch'
        if mode != 'verify':
            assert all_runs == runs and all_passes == passes, 'foreign aggregator/test in focused log'
            expected = Counter({PACKAGE + k: 1 for k in runs if k != aggregator})
            assert executed == expected, aggregator + ': executed mismatch (aggregator forbidden)'
        print('PASS exact ' + str(COUNTS[aggregator]) + ' ' + aggregator + ' root RUN/PASS' + ('/executed' if mode != 'verify' else '') + '; documented nested cases checked separately; no skips/fails')
    if mode == 'verify':
        assert not any(k.startswith('TestRegularObservation') and k != 'TestRegularObservationBoundary'
                       and not k.startswith('TestRegularObservationBoundary/')
                       for k in all_runs.keys() | all_passes.keys()), 'unexpected regular supporting test'
    if mode in ('coordinator-focused', 'verify'):
        check_coordinator(text, mode)
    if mode in ('flock-focused', 'verify'):
        check_flock(text, mode)
    if mode in ('private-focused', 'verify'):
        check_umask(text, mode)
    if mode == 'verify':
        expected = Counter({name: 1 for root, children in SUPPORT.items() for name in [root] + [root + '/' + child for child in children]})
        runs = Counter({k: v for k, v in all_runs.items() if k.startswith('TestPrivate') and not k.startswith('TestPrivateCreateOpenBoundary/') and k != 'TestPrivateCreateOpenBoundary'})
        passes = Counter({k: v for k, v in all_passes.items() if k.startswith('TestPrivate') and '/' in k and not k.startswith('TestPrivateCreateOpenBoundary/')})
        passes.update({k: v for k, v in top_passes.items() if k.startswith('TestPrivate') and k != 'TestPrivateCreateOpenBoundary'})
        assert runs == passes == expected, 'private supporting RUN/PASS inventory mismatch'
        print('PASS exact ' + str(len(SUPPORT)) + ' supporting private roots / ' + str(len(expected)) + ' total nodes; outer inert helper counted once, not five subprocesses')
    if mode == 'verify':
        expected = Counter({name: 1 for root, children in FLOCK_SUPPORT.items() for name in [root] + [root + '/' + child for child in children]})
        runs = Counter({k: v for k, v in all_runs.items() if k.startswith('TestFlock') and k != 'TestFlockBoundary' and not k.startswith('TestFlockBoundary/')})
        passes = Counter({k: v for k, v in all_passes.items() if k.startswith('TestFlock') and '/' in k and not k.startswith('TestFlockBoundary/')})
        passes.update({k: v for k, v in top_passes.items() if k.startswith('TestFlock') and k != 'TestFlockBoundary'})
        assert runs == passes == expected, 'flock supporting RUN/PASS inventory mismatch'
        print('PASS exact 3 supporting flock roots / 19 nodes; outer inert FlockProcess counted once, not four subprocesses')
    if mode == 'verify':
        expected = Counter({'TestCoordinatorProcess': 1})
        runs = Counter({k: v for k, v in all_runs.items() if k.startswith('TestCoordinator') and k != 'TestCoordinatorBoundary' and not k.startswith('TestCoordinatorBoundary/')})
        passes = Counter({k: v for k, v in all_passes.items() if k.startswith('TestCoordinator') and '/' in k and not k.startswith('TestCoordinatorBoundary/')})
        passes.update({k: v for k, v in top_passes.items() if k.startswith('TestCoordinator') and k != 'TestCoordinatorBoundary'})
        assert runs == passes == expected, 'coordinator supporting RUN/PASS inventory mismatch'
        print('PASS exact coordinator supporting root/node: outer inert TestCoordinatorProcess once, not embedded child PASS')
    print('Limits: TEST-only private security boundary is bounded integration, NOT canonical CLI integration. Injected negative facts are not native positive proof.')

def check_coordinator(text, mode):
    """Bind available stdout fields; private IPC and parent selected ID are not logs."""
    caller = r'coordinator_test\.go:[0-9]+'
    headers = list(re.finditer(r'^[ \t]+' + caller + r': real process read-only flock proof:[ \t]*$', text, re.M))
    assert len(headers) == 1, 'coordinator proof header missing/duplicate'
    assert len(re.findall(r'^[ \t]+coordinator_process_test\.go:[0-9]+: bounded coordinator child ', text, re.M)) == 1, 'coordinator native outcome missing/duplicate'
    assert not re.search(r'^[ \t]*\[compiler output truncated:', text, re.M), 'coordinator reported child capture truncation'
    root = 'TestCoordinatorBoundary'
    case = root + '/UserBeforeResolution'
    pass_indent = r'[ \t]*' if mode == 'verify' else ''
    events = list(re.finditer(r'^=== RUN[ \t]+(\S+)[ \t]*$|^' + pass_indent
                             + r'--- PASS: (\S+) \([^\n]*\)[ \t]*$', text, re.M))
    # Only the exact indented child helper PASS is not an outer canonical event.
    events = [e for e in events if not (e[2] == 'TestCoordinatorProcess' and e[0].startswith((' ', '\t')))]
    header = headers[0]
    preceding = [e for e in events if e.start() < header.start()]
    assert preceding and preceding[-1][1] == case, 'coordinator header wrong active case'
    runs = {e[1]: e.start() for e in preceding if e[1]}
    assert root in runs and runs[root] < runs[case], 'coordinator missing/unordered ancestor'
    assert not any(e[2] in (root, case) for e in preceding), 'coordinator closed case/ancestor'
    identity = r'\{Device:[0-9]+ Inode:[0-9]+ Kind:16384 Valid:true\}'
    facts = (r'\{identity:' + identity + r' uid:[0-9]+ gid:[0-9]+ mode:448 nlink:[0-9]+ '
             r'filesystem:\{state:2 model:[^\s{}]+ kind:[0-9]+ flags:[0-9]+ id:\{\[[-0-9 ]+\]\}\} '
             r'security:\{state:1 mechanism:[^\s{}]+\}\}')
    fixture = text[preceding[-1].end():header.start()]
    parents = list(re.finditer(r'^[ \t]+' + caller + r': bounded native fixture os=\S+ arch=\S+ path=(?P<path>/\S+) facts=(?P<facts>' + facts + r')[ \t]*$', fixture, re.M))
    assert len(parents) == 1, 'coordinator parent native fixture missing/duplicate/invalid'
    parent = parents[0]
    end = next((e.start() for e in events if e.start() > header.end()), len(text))
    block = text[header.end():end]
    child_runs = list(re.finditer(r'^[ \t]+=== RUN[ \t]+(\S+)[ \t]*$', block, re.M))
    child_passes = list(re.finditer(r'^[ \t]+--- PASS: (\S+) \([^\n]*\)[ \t]*$', block, re.M))
    terminals = list(re.finditer(r'^[ \t]+PASS[ \t]*$', block, re.M))
    quoted = r'"(?:[^"\\\n]|\\.)*"'
    outcomes = list(re.finditer(r'^[ \t]+coordinator_process_test\.go:[0-9]+: bounded coordinator child root=(?P<root>' + quoted
                               + r') identity=(?P<identity>' + identity + r') facts=(?P<facts>' + facts
                               + r') config-selection=(?P<selection>' + quoted + r') binding=\{logical:(?P<logical>/\S+) physical:(?P<physical>/\S+) identity:(?P<binding>' + identity + r')\}[ \t]*$', block, re.M))
    assert [m[1] for m in child_runs] == ['TestCoordinatorProcess'], 'coordinator child RUN mismatch'
    assert [m[1] for m in child_passes] == ['TestCoordinatorProcess'], 'coordinator child PASS mismatch'
    assert len(outcomes) == len(terminals) == 1, 'coordinator outcome/terminal missing/duplicate/invalid'
    out = outcomes[0]
    assert child_runs[0].start() < out.start() < child_passes[0].start() < terminals[0].start(), 'coordinator child transcript order mismatch'
    path = json.loads(out['root'])
    assert path == parent['path'] and path not in ('/', '/tmp', '/private/tmp') and not path.endswith('/'), 'coordinator root path mismatch'
    assert out['identity'] == re.search(identity, parent['facts'])[0] == re.search(identity, out['facts'])[0], 'coordinator root identity mismatch'
    # Bootstrap/repository fixture creation legitimately changes the directory nlink.
    stable = lambda value: re.sub(r' nlink:[0-9]+ ', ' nlink:OBSERVED ', value)
    assert stable(out['facts']) == stable(parent['facts']), 'coordinator stable native root facts mismatch'
    assert json.loads(out['selection']) == path + '/z' == out['logical'] == out['physical'], 'coordinator config selection/binding mismatch'
    assert out['binding'] != out['identity'], 'coordinator repository binding is root identity'
    assert len(re.findall(r'^[ \t]+=== RUN[ \t]+TestCoordinatorProcess[ \t]*$', text, re.M)) == 1, 'coordinator orphan child RUN'
    assert len(re.findall(r'^[ \t]+--- PASS: TestCoordinatorProcess \([^\n]*\)[ \t]*$', text, re.M)) == 1, 'coordinator orphan child PASS'
    print('PASS coordinator exact active UserBeforeResolution child RUN/native root identity/facts/selection/binding/PASS/terminal block')
    print('Correlation limits: source-bound parent-case PASS asserts private-pipe handshakes and differing explicit a/config z choices; no raw IPC or independent parent selected ID. Whole matching fixture/proof relocation is not cryptographic case provenance; private test boundary is not canonical CLI proof.')


def check_flock(text, mode):
    """Correlate captured child stdout, never pretend private IPC was logged."""
    caller = r'flock(?:_process)?_test\.go:[0-9]+'
    headers = list(re.finditer(r'^[ \t]+' + caller + r': real process read-only flock proof:[ \t]*$', text, re.M))
    assert len(headers) == 4, 'flock proof headers missing/duplicate'
    assert len(re.findall(r'^[ \t]+flock_process_test\.go:[0-9]+: native child ', text, re.M)) == 4, 'flock native outcomes missing/duplicate'
    root = 'TestFlockBoundary'
    pass_indent = r'[ \t]*' if mode == 'verify' else ''
    events = list(re.finditer(r'^=== RUN[ \t]+(\S+)[ \t]*$|^' + pass_indent
                             + r'--- PASS: (\S+) \([^\n]*\)[ \t]*$', text, re.M))
    # Full Go nested PASS is canonical except this exact indented subprocess helper.
    # A column-zero helper PASS or any foreign canonical event still closes the block.
    events = [e for e in events if not (e[2] == 'TestFlockProcess' and e[0].startswith((' ', '\t')))]
    seen = Counter()
    identity = r'\{Device:([0-9]+) Inode:([0-9]+) Kind:([0-9]+) Valid:true\}'
    for header in headers:
        preceding = [event for event in events if event.start() < header.start()]
        assert preceding and preceding[-1][1], 'flock header not bound to canonical case RUN'
        event = preceding[-1]
        name = event[1]
        leaf = name.removeprefix(root + '/')
        assert name == root + '/' + leaf and leaf in FLOCK_CHILDREN, 'flock header wrong outer case: ' + name
        seen[leaf] += 1
        ancestors = [root] + ([root + '/ReturnedLeasePinsAncestry'] if '/' in leaf else []) + [name]
        runs = {e[1]: e.start() for e in preceding if e[1]}
        assert all(a in runs for a in ancestors) and all(runs[a] < runs[b] for a, b in zip(ancestors, ancestors[1:])), 'flock header unordered/missing ancestors'
        assert not any(e[2] in ancestors for e in preceding), 'flock header outside active case/ancestor window'
        fixture = text[event.end():header.start()]
        fixtures = list(re.finditer(r'^[ \t]+' + caller + r': bounded native fixture os=\S+ arch=\S+ path=(\S+) facts=\{identity:(' + identity + r') [^\n]+\}[ \t]*$', fixture, re.M))
        assert len(fixtures) == 1, 'flock parent native fixture missing/duplicate in active case'
        parent = fixtures[0]
        assert parent[5] == '16384' and parent[1].startswith('/'), 'flock parent fixture identity/path invalid'
        end = next((e.start() for e in events if e.start() > header.end()), len(text))
        block = text[header.end():end]
        child_runs = list(re.finditer(r'^[ \t]+=== RUN[ \t]+(\S+)[ \t]*$', block, re.M))
        child_passes = list(re.finditer(r'^[ \t]+--- PASS: (\S+) \([^\n]*\)[ \t]*$', block, re.M))
        terminals = list(re.finditer(r'^[ \t]+PASS[ \t]*$', block, re.M))
        outcomes = list(re.finditer(r'^[ \t]+flock_process_test\.go:[0-9]+: native child directory=(true|false) root-facts=\{identity:(' + identity + r') [^\n]+\} selected=(' + identity + r')[ \t]*$', block, re.M))
        assert [m[1] for m in child_runs] == ['TestFlockProcess'], 'flock child entry mismatch'
        assert [m[1] for m in child_passes] == ['TestFlockProcess'], 'flock child PASS mismatch'
        assert len(terminals) == len(outcomes) == 1, 'flock child native outcome/terminal PASS mismatch'
        outcome = outcomes[0]
        assert child_runs[0].start() < outcome.start() < child_passes[0].start() < terminals[0].start(), 'flock child outcome ordering mismatch'
        assert outcome[1] == FLOCK_CHILDREN[leaf], 'flock child variant not bound to outer case'
        assert outcome[2] == parent[2], 'flock child root identity not bound to parent native fixture'
        assert outcome[9] == ('16384' if outcome[1] == 'true' else '32768'), 'flock child selected identity kind mismatch'
    assert seen == Counter({leaf: 1 for leaf in FLOCK_CHILDREN}), 'flock proof missing/duplicate/wrong case'
    assert len(re.findall(r'^[ \t]+=== RUN[ \t]+TestFlockProcess[ \t]*$', text, re.M)) == 4, 'flock child entries outside proof blocks'
    assert len(re.findall(r'^[ \t]+--- PASS: TestFlockProcess \([^\n]*\)[ \t]*$', text, re.M)) == 4, 'flock child PASS outside proof blocks'
    print('PASS four case/type/root-identity-bound child RUN/native outcome/PASS/terminal PASS blocks; parent PASS plus source asserts private-pipe handshakes, NOT a raw IPC transcript')
    print('Correlation limit: selected identity equality is source-asserted, not independently parent-logged; co-relocation of a whole matching fixture+proof is not independently attributable from these logs.')


def check_umask(text, mode):
    headers = list(re.finditer(r'^[ \t]+private_umask_test\.go:[0-9]+: isolated umask (\S+):[ \t]*$', text, re.M))
    assert Counter(m[1] for m in headers) == Counter({m: 1 for m in UMASKS}), 'umask subprocess headers missing/duplicate/foreign'
    outcomes = re.findall(r'^[ \t]+private_umask_test\.go:[0-9]+: native umask (.*)$', text, re.M)
    assert Counter(outcomes) == Counter({m + ' file=0600 directory=' + d + ' retained=true': 1 for m, d in UMASKS.items()}), 'umask native outcomes mismatch'
    root = 'TestPrivateCreateOpenBoundary'
    parent = root + '/RestrictiveUmaskDirectoryRetained'
    pass_indent = r'[ \t]*' if mode == 'verify' else ''
    # Only canonical outer events delimit the window; indented child RUN/PASS
    # belongs to the transcript, not the enclosing private case.
    events = list(re.finditer(r'^=== RUN[ \t]+(\S+)[ \t]*$|^' + pass_indent
                             + r'--- PASS: (' + root + r'(?:/\S+)?) \([^\n]*\)[ \t]*$', text, re.M))
    for header in headers:
        name = parent + '/' + header[1]
        preceding = [event for event in events if event.start() < header.start()]
        assert preceding and preceding[-1][1] == name, 'umask header not bound to canonical case RUN: ' + name
        runs = {event[1]: event.start() for event in preceding if event[1]}
        # check_text already requires singleton events: no completed ancestor or
        # matching case may be reopened by moving its PASS before its RUN.
        assert (root in runs and parent in runs and runs[root] < runs[parent] < runs[name]
                and not any(event[2] in (root, parent, name) for event in preceding)), 'umask header outside active case/ancestor window: ' + name
        # Source logs each complete child transcript before the next outer event.
        end = next((event.start() for event in events if event.start() > header.end()), len(text))
        block = text[header.end():end]
        assert re.findall(r'^[ \t]+=== RUN[ \t]+(\S+)[ \t]*$', block, re.M) == ['TestPrivateUmaskProcess'], 'umask child entry mismatch'
        assert re.findall(r'^[ \t]+--- PASS: (\S+) \([^\n]*\)[ \t]*$', block, re.M) == ['TestPrivateUmaskProcess'], 'umask child PASS mismatch'
        assert len(re.findall(r'^[ \t]+PASS[ \t]*$', block, re.M)) == 1, 'umask child terminal PASS mismatch'
        expected = header[1] + ' file=0600 directory=' + UMASKS[header[1]] + ' retained=true'
        assert re.findall(r'^[ \t]+private_umask_test\.go:[0-9]+: native umask (.*)$', block, re.M) == [expected], 'umask outcome not bound to child entry'
    print('PASS five isolated umask child entries/native outcomes/PASS: 000/077/200/700/777; separate from outer inert helper')


# Fixed retention export. Metadata/bytes observation is not a filesystem snapshot,
# cryptographic case provenance, or later cleanup/signal authority. No atime check.
EXPORT_LIMITS = {'roots': 512, 'entries': 20000, 'depth': 8, 'file': 2*1024**2,
                 'bytes': 64*1024**2, 'wire': 96*1024**2, 'seconds': 120}
# Two passes over the168 source-declared root references in the observed complete
# Darwin fixture inventory predict336, NOT acceptance.512 bounds that union and
# early-failure retained setup; new inventory requires re-review, never truncation.
EXPORT_ROOT_DECLARATIONS = {'TestFocusedProtocolRecordFraming': 15, 'TestFocusedProtocolPublication': 4, 'TestFocusedProtocolWakeClassification': 12, 'TestFocusedProtocolDescriptorClosure': 1, 'TestFocusedProtocolSolePulseWriter': 1, 'TestFocusedProtocolPulseWake': 1, 'TestFocusedProtocolNoPulseNegative': 1, 'TestFocusedProtocolPulseCancellation': 4, 'TestFocusedProtocolParentAbort': 1, 'TestFocusedProtocolPendingContinuation': 4, 'TestFocusedProtocolPulseLifecycle': 9, 'TestFocusedProtocolIdentityRecords': 9, 'TestFocusedProtocolOuterIdentity': 6, 'TestFocusedProtocolAuthorizationHealth': 10, 'TestFocusedProtocolNegativeRetention': 5, 'TestFocusedProtocolFatalOwner': 2, 'TestFocusedProtocolFatalOwnerFixture': 0, 'TestFocusedProtocolStartFailure': 1, 'TestFocusedProtocolOwnerOnce': 2, 'TestFocusedProtocolBoundedCapture': 0, 'TestFocusedProtocolCleanupUncertainty': 18, 'TestFocusedProtocolCaptureEOF': 1, 'TestFocusedProtocolEventTermination': 4, 'TestFocusedProtocolEventByteBudget': 3, 'TestFocusedProtocolEventChannel': 12, 'TestFocusedTaskProtocol': 42}
REGULAR_ROLES = set('anchor anchor-record anchor-waited boundary capture-exit-ready capture-peer-reaped capture-writer-closed capture-writer-ready captured-output cleanup-complete closed continue failure fatal-unwind go original partial pre-start-refusal pulse-result queued-before-consumption ready real-run-return received received-again record sentinel signals start-failure target task.sh teardown-KILL teardown-TERM work-started wrapper-start wrapper-trace wrapper-wait process-events protocol-runner protocol-spawn-intent protocol-spawn child-start child-spawn-intent child-spawn child-reaped reaped descendant-start runner-start child-closed'.split())


def export_metadata(st):
    return {k: getattr(st, 'st_'+k) for k in ('dev','ino','mode','uid','gid','nlink','size','mtime_ns','ctime_ns')}


def export_encoded(data):
    return {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(),
            'data': base64.b64encode(data).decode('ascii')}


def export_decoded(obj, limit):
    assert set(obj) == {'bytes','sha256','data'}, 'export payload keys'
    assert type(obj['bytes']) is int and 0 <= obj['bytes'] <= limit, 'export file limit'
    assert isinstance(obj['data'],str) and len(obj['data']) <= 4*((limit+2)//3), 'export encoded limit'
    data = base64.b64decode(obj['data'], validate=True)
    assert obj == export_encoded(data), 'export byte/hash mismatch'
    return data


def _check_positive_removal(case, outcome, text):
    assert case == 'TestFocusedProtocolNegativeRetention/positive_removal/registered_cleanup', 'unapproved positive removal'
    assert outcome.groups()[3:6] == ('Wait result=<nil>','true','false'), 'unproven positive removal'
    assert re.search(r'^[ \t]*--- PASS: TestFocusedProtocolNegativeRetention/positive_removal \([0-9.]+s\)$',text,re.M), 'removal parent did not PASS'


def protocol_case_events(text, lane):
    """Serial Go grammar: qualified RUN/NAME, immediate or deferred outcomes.

    NAME is the only authority to switch live logging back before an outcome.
    Deferred child outcomes never resurrect an already closed parent. Expected
    fatal subprocess output is isolated by its original strict block validator.
    """
    assert len(text.encode('utf-8')) <= OUTPUT_LIMIT, 'protocol text bound'
    inventory = protocol_inventory(lane)
    allowed = {name for root, children in inventory.items() for name in [root]+[root+'/'+c for c in children]}
    def qualified(name):
        assert len(name) <= 4096, 'protocol case length'
        normalized = re.sub(r'Context[0-9]+/001/', 'ContextROOT/001/', name)
        assert normalized in allowed, 'foreign qualified protocol case'
        return name
    seen, closed, stack = set(), set(), []
    fatal = 'TestFocusedProtocolFatalOwner'
    fatal_lines = set()
    if re.search(r'^[ \t]+--- FAIL: TestFocusedProtocolFatalOwnerFixture ', text, re.M):
        fatal_child_lines(text, True)
        lines = text.splitlines()
        parent_start = next(i for i,l in enumerate(lines) if re.fullmatch(r'=== RUN[ \t]+'+fatal,l))
        header = next(i for i,l in enumerate(lines) if i > parent_start and re.fullmatch(r'    '+PROTOCOL_CALLER+r': preserved final stdout/stderr root='+PROTOCOL_ROOT+r' truncated=false:',l) and any(re.fullmatch(r'[ \t]+--- FAIL: TestFocusedProtocolFatalOwnerFixture \([0-9.]+s\)',x) for x in lines[i+1:]))
        terminal = next(i for i in range(header+1,len(lines)) if lines[i] == '        FAIL')
        fatal_lines = set(range(header+1,terminal+1))
    count = 0
    for number, line in enumerate(text.splitlines()):
        assert len(line) <= 65536, 'protocol line bound'
        if number in fatal_lines:
            yield line, fatal, True
            continue
        event = re.fullmatch(r'=== (RUN|NAME)[ \t]+(\S+)[ \t]*', line)
        outcome = re.fullmatch(r'( *)--- (PASS|FAIL|SKIP): (\S+) \([0-9]+(?:\.[0-9]+)?s\)', line)
        if event or outcome:
            name = event[2] if event else outcome[3]
            if not name.startswith(('TestFocusedProtocol','TestFocusedTaskProtocol','TestFocusedCancellation','TestFocusedChild')):
                assert not stack, 'foreign event within live protocol case'
                continue
            name = qualified(name)
            count += 1
            assert count <= 4096, 'protocol event bound'
            if event and event[1] == 'RUN':
                assert name not in seen, 'duplicate protocol RUN'
                ancestors = [n for n in seen-closed if name.startswith(n+'/')]
                parent = max(ancestors,key=len) if ancestors else None
                if '/' in name:
                    assert parent is not None and parent in stack, 'missing/live protocol parent'
                    stack = stack[:stack.index(parent)+1]
                else:
                    assert not stack and seen == closed, 'overlapping protocol roots'
                stack.append(name);seen.add(name)
                assert len(stack) <= 32, 'protocol nesting bound'
            elif event:
                assert name in seen-closed, 'NAME outside live protocol case'
                ancestry = sorted((n for n in seen-closed if name == n or name.startswith(n+'/')),key=len)
                assert stack and ancestry[0] == stack[0], 'NAME switches protocol root'
                stack = ancestry
            else:
                assert name in seen and name not in closed, 'unbound/duplicate protocol outcome'
                depth = sum(name.startswith(parent+'/') for parent in seen)
                # Focused output is flattened; full verbose Go uses four spaces/depth.
                assert not outcome[1] or len(outcome[1]) == 4*depth, 'protocol outcome indentation'
                closed.add(name)
                if name in stack: stack = stack[:stack.index(name)]
            continue
        if re.match(r'^[ \t]*(?:===|---)',line) and (stack or 'TestFocused' in line):
            raise AssertionError('ambiguous protocol event grammar')
        yield line, stack[-1] if stack else None, False
    assert seen == closed and not stack, 'unfinished protocol RUN'


def protocol_root_roles(logs, require_complete=True):
    roots = {}
    for lane, text in logs:
        before_roots = set(roots)
        for line, active, nested in protocol_case_events(text,lane):
            declaration = re.fullmatch(r'    '+PROTOCOL_CALLER+r': private protocol evidence: ('+PROTOCOL_ROOT+r')',line)
            outcome = re.fullmatch(r'[ \t]+'+PROTOCOL_CALLER+r': protocol root=('+PROTOCOL_ROOT+r') phase="([^"]*)" epoch=(\S*) outcome=(.*?) cleanup-complete=(true|false) retained=(true|false); identities are historical observations, never later signal authority',line)
            if declaration:
                path = declaration[1]
                assert active and active.startswith(('TestFocusedProtocol','TestFocusedTaskProtocol/')) and not nested, 'root outside fixture case'
                assert path not in roots and len(roots) < EXPORT_LIMITS['roots'], 'duplicate/excess root declaration'
                roots[path] = {'case':active,'lane':lane,'outcome':None,'removed':False,'nested_fatal':False}
            elif outcome:
                path = outcome[1]
                assert path in roots and roots[path]['outcome'] is None, 'unbound/duplicate root outcome'
                assert roots[path]['lane'] == lane and roots[path]['case'] == active, 'root outcome outside qualified owner'
                assert nested == (active == 'TestFocusedProtocolFatalOwner' and outcome[2] == 'reader'), 'fatal outcome scope'
                roots[path]['outcome'] = {'phase':outcome[2],'epoch':outcome[3],'wait':outcome[4],'complete':outcome[5]=='true','retained':outcome[6]=='true'}
                if not roots[path]['outcome']['retained']:
                    _check_positive_removal(roots[path]['case'],outcome,text)
                    roots[path]['removed'] = True
                roots[path]['nested_fatal'] = nested
            elif 'protocol root=' in line or 'private protocol evidence:' in line:
                raise AssertionError('malformed protocol root record')
        if require_complete:
            inventory = protocol_inventory(lane)
            expected = Counter({name: EXPORT_ROOT_DECLARATIONS.get(name, 0) for name in inventory if EXPORT_ROOT_DECLARATIONS.get(name, 0)})
            observed = Counter(roots[path]['case'].split('/')[0] for path in set(roots)-before_roots)
            assert observed == expected, 'missing/extra source-declared fixture roots'
    return roots


def diagnostic_root_roles(logs):
    """Raw declarations survive failed/ambiguous attribution; null is not a case."""
    roots, issues = {}, []
    for lane, text in logs:
        declared = re.findall(r'^    '+PROTOCOL_CALLER+r': private protocol evidence: ('+PROTOCOL_ROOT+r')$',text,re.M)
        assert len(roots)+len(declared) <= EXPORT_LIMITS['roots'], 'diagnostic root bound'
        try:
            attributed = protocol_root_roles([(lane,text)], require_complete=False)
        except (AssertionError, ValueError):
            attributed = {}
            issues.append({'lane':lane,'reason':'ambiguous-or-unfinished-attribution'})
        for path in declared:
            if path in roots:
                issues.append({'lane':lane,'root':path,'reason':'duplicate-declaration'})
                roots[path] = {'case':None,'lane':None,'outcome':None,'removed':False,'nested_fatal':False}
            else:
                roots[path] = attributed.get(path, {'case':None,'lane':lane,'outcome':None,'removed':False,'nested_fatal':False})
    return roots, issues


def protocol_entry_role(case, relative, kind):
    """Only source-declared nonregular *input fixtures*, never journal readers."""
    parts = relative.split('/')
    assert len(parts) <= EXPORT_LIMITS['depth'] and all(re.fullmatch(r'[A-Za-z0-9_.-]+',p) and p not in ('.','..') for p in parts), 'export unsafe path'
    if len(parts) == 1 and re.fullmatch(r'dotty-focused\.[A-Za-z0-9]{8}',parts[0]):
        assert case.startswith(('TestFocusedTaskProtocol/','TestFocusedProtocolPulse','TestFocusedProtocolNoPulseNegative')), 'foreign wrapper fixture directory'
        assert kind == 'directory', 'wrapper fixture directory type'
        return 'wrapper-fixture'
    if len(parts) == 2 and re.fullmatch(r'dotty-focused\.[A-Za-z0-9]{8}',parts[0]):
        protocol_entry_role(case,parts[0],'directory')
        assert parts[1] in ('status','control','focused'), 'unknown wrapper input'
        assert kind == ('regular' if parts[1]=='focused' else 'fifo'), 'wrapper input type'
        return 'wrapper-input'
    assert len(parts) == 1, 'unexpected nested entry'
    name = parts[0]
    if case == 'TestFocusedProtocolPublication' and name in ('directory','FIFO','symlink'):
        assert kind == {'directory':'directory','FIFO':'fifo','symlink':'symlink'}[name], 'publication input type'
        return 'negative-input'
    for prefix,role,suffix in [('TestFocusedProtocolIdentityRecords/','child-start',''),
                               ('TestFocusedProtocolCleanupUncertainty/complete_pre-start_witness/','pre-start-refusal','_witness')]:
        if case in (prefix+'FIFO'+suffix,prefix+'directory'+suffix,prefix+'symlink'+suffix) and name == role:
            assert kind == {'FIFO':'fifo','directory':'directory','symlink':'symlink'}[case[len(prefix):].removesuffix(suffix)], 'negative record input type'
            return 'negative-input'
    assert kind == 'regular', 'nonregular required journal/unknown entry'
    assert name in REGULAR_ROLES or re.fullmatch(r'\.publication-[0-9]+',name) or re.fullmatch(r'(?:read-enter|read-return)-(?:[1-9][0-9]?|1[01][0-9]|12[0-8])',name) or re.fullmatch(r'cancel-[1-8]',name), 'unknown regular journal role'
    return 'publication' if name.startswith('.publication-') else 'journal'


def protocol_required_records(role):
    case, outcome = role['case'], role['outcome']
    required = set()
    top = case.split('/')[0]
    owner_roots = {'TestFocusedProtocol'+name for name in ('RecordFraming','WakeClassification','DescriptorClosure','SolePulseWriter','PulseWake','NoPulseNegative','PulseCancellation','ParentAbort','PendingContinuation','NegativeRetention','FatalOwner','OwnerOnce','CaptureEOF')}
    owner_case = top in owner_roots or case.startswith('TestFocusedTaskProtocol/') or case in ('TestFocusedProtocolOuterIdentity/publication_failure_prevents_spawn','TestFocusedProtocolOuterIdentity/spawn-intent_failure_prevents_spawn','TestFocusedProtocolEventChannel/anchor_inheritance','TestFocusedProtocolEventChannel/Go_fixture_boundary_does_not_start_pulses')
    if owner_case: assert outcome is not None, 'missing source-required owner outcome'
    if case == 'TestFocusedProtocolEventChannel/Go_fixture_boundary_does_not_start_pulses':
        assert outcome['complete'] and outcome['retained'], 'fixture boundary cleanup outcome'
        required.add('boundary')
    if case == 'TestFocusedProtocolCleanupUncertainty': required.add('real-run-return')
    if case.startswith('TestFocusedProtocolCleanupUncertainty/complete_pre-start_witness/'):
        witness_case=case.rsplit('/',1)[1]
        if witness_case != 'missing_witness': required.add('pre-start-refusal')
        if witness_case in ('competing_return_record','contradictory_return_record'): required.add('real-run-return')
        if witness_case == 'symlink_witness': required.add('target')
        conflict={'child_start':'child-start','spawn_intent':'child-spawn-intent','spawn_completion':'child-spawn',
                  'descendant_start':'descendant-start','descendant_readiness':'ready'}.get(witness_case)
        if conflict: required.add(conflict)
    if top == 'TestFocusedProtocolPulseLifecycle': required.add('pulse-result')
    if top == 'TestFocusedProtocolOuterIdentity': required.add('anchor-record')
    if top == 'TestFocusedProtocolAuthorizationHealth' and not case.endswith('/empty_channel_loss'):
        required |= {'boundary','read-enter-1','read-return-1','cancel-1'}
    if top in ('TestFocusedProtocolPulseWake','TestFocusedProtocolNoPulseNegative','TestFocusedProtocolPulseCancellation'):
        required |= {'boundary','read-enter-1'}
        if top != 'TestFocusedProtocolNoPulseNegative': required.add('read-return-1')
    if outcome is not None:
        required |= {'wrapper-start','wrapper-wait','pulse-result','captured-output'}
        if outcome['complete']: required.add('cleanup-complete')
    if case.startswith('TestFocusedTaskProtocol/') or case.split('/')[0] in ('TestFocusedProtocolPulseWake','TestFocusedProtocolPulseCancellation','TestFocusedProtocolNoPulseNegative'):
        assert outcome is not None and outcome['retained'], 'matrix/sensitivity outcome missing'
        required |= {'go','task.sh','wrapper-trace'}
    if role['nested_fatal']: required |= {'fatal-unwind','read-enter-1','read-return-1','cleanup-complete','captured-output'}
    if case == 'TestFocusedProtocolStartFailure': required.add('start-failure')
    if case == 'TestFocusedProtocolPublication': required |= {'record','partial','directory','FIFO','symlink'}
    if case.startswith('TestFocusedProtocolIdentityRecords/'): required.add('child-start')
    if case.startswith('TestFocusedProtocolRecordFraming/'): required.add('record')
    if case.startswith('TestFocusedProtocolCaptureEOF'):
        required |= {'capture-peer-reaped','capture-writer-closed','capture-writer-ready','capture-exit-ready','captured-output'}
        assert outcome is not None and not outcome['complete'] and outcome['retained'], 'capture-negative outcome'
    return required


def diagnostic_entry_role(case, relative, kind):
    if case is not None:
        return protocol_entry_role(case, relative, kind)
    # Unattributed artifacts may be preserved, never accepted as fixture proof.
    if '/' not in relative and relative in ('directory','FIFO','symlink','pre-start-refusal','child-start') and kind in ('directory','fifo','symlink'):
        return 'negative-input'
    if re.fullmatch(r'dotty-focused\.[A-Za-z0-9]{8}(?:/(?:status|control|focused))?', relative):
        return protocol_entry_role('TestFocusedTaskProtocol/unattributed',relative,kind)
    return protocol_entry_role('',relative,kind)


def check_export_records(roots, entries, require_complete=True):
    grouped = {p:{} for p in roots}
    total = 0
    hardlinks = {}
    _check_export_entry_count(entries)
    # Validate complete permitted hardlink scope before decoding ANY inode payload.
    aliases = {}
    for entry in entries:
        metadata = entry['metadata']
        assert metadata['uid'] == PRIVATE_UID, 'unexpected export UID'
        if entry['kind'] == 'regular':
            assert entry['root'] in roots, 'export duplicate/foreign entry'
            (protocol_entry_role if require_complete else diagnostic_entry_role)(
                roots[entry['root']]['case'], entry['path'], entry['kind'])
            key = (metadata['dev'], metadata['ino'])
            aliases.setdefault(key, []).append(entry)
    for group in aliases.values():
        names = {(entry['root'], entry['path']) for entry in group}
        assert len(names) == len(group) == group[0]['metadata']['nlink'], 'unresolved export hardlinks'
        assert all(entry['metadata'] == group[0]['metadata'] for entry in group), 'changed hardlink metadata'
    for entry in entries:
        assert set(entry) == {'root','path','kind','metadata','payload'}, 'export entry keys'
        root,path,kind,metadata = (entry[k] for k in ('root','path','kind','metadata'))
        assert root in roots and not roots[root]['removed'] and path not in grouped[root], 'export duplicate/foreign entry'
        assert set(metadata) == {'dev','ino','mode','uid','gid','nlink','size','mtime_ns','ctime_ns'} and all(type(v) is int and v>=0 for v in metadata.values()), 'export metadata fields'
        assert metadata['ino']>0 and metadata['nlink']>0, 'export metadata identity'
        expected_kind = {stat.S_IFREG:'regular',stat.S_IFDIR:'directory',stat.S_IFIFO:'fifo',stat.S_IFLNK:'symlink'}.get(stat.S_IFMT(metadata['mode']))
        assert kind == expected_kind, 'export mode/type mismatch'
        if path == '':
            assert kind == 'directory' and stat.S_IMODE(metadata['mode']) == 0o700 and metadata['uid']==PRIVATE_UID, 'nonprivate root'
        else:
            (protocol_entry_role if require_complete else diagnostic_entry_role)(roots[root]['case'],path,kind)
            parent = path.rpartition('/')[0]
            assert parent in grouped[root] and grouped[root][parent]['kind']=='directory', 'export missing parent'
        if kind == 'regular':
            data = export_decoded(entry['payload'],EXPORT_LIMITS['file'])
            assert len(data)==metadata['size'], 'export stat/bytes mismatch'
            identity = (metadata['dev'],metadata['ino'])
            current = (metadata,entry['payload'])
            assert identity not in hardlinks or hardlinks[identity] == current, 'changed hardlinked regular bytes/metadata'
            hardlinks[identity] = current
            total += len(data)
        else:
            assert entry['payload'] is None, 'nonregular content forbidden'
        grouped[root][path] = entry
    assert total <= EXPORT_LIMITS['bytes'], 'export byte limit'
    for root,role in roots.items():
        files = grouped[root]
        if role['removed']:
            assert not files, 'removed root has entries'
            continue
        assert '' in files, 'missing required root'
        if not require_complete:
            continue
        required = protocol_required_records(role)
        assert required <= files.keys(), 'missing required journals: '+repr(sorted(required-files.keys()))
        for name in required:
            if name not in ('FIFO','directory','symlink','child-start','pre-start-refusal'):
                assert files[name]['kind']=='regular', 'required record nonregular'
        if role['outcome'] and role['outcome']['complete']:
            assert export_decoded(files['cleanup-complete']['payload'],1024)==b'quiescent', 'false cleanup bytes'
        if role['nested_fatal']:
            assert export_decoded(files['fatal-unwind']['payload'],1024)==b'wait-calls=1;complete=true;retained=true', 'fatal unwind bytes'
    return total



def check_fatal_owner_witnesses(roots, entries):
    """Conditional FD9 publication proof, not the stdout/stderr v2 receipt.

    Candidate task_unix_test.go:1098-1100,1138-1141: failed FD9 printf
    parks; advancement into read proves successful send. 2530-2557 joins
    event drain/health before complete; 2482-2489 and 2623-2630 bind the
    canonical same-root cleanup/fatal-unwind records. No universal enter2.
    """
    for root,role in roots.items():
        if not role['nested_fatal']:continue
        epoch=root.rsplit('/',1)[1]
        outcome=role['outcome']
        assert role['case']=='TestFocusedProtocolFatalOwner' and not role['removed'], 'fatal witness owner'
        assert outcome and outcome.get('phase')=='reader' and outcome.get('epoch')==epoch and outcome.get('complete') is True and outcome.get('retained') is True, 'fatal witness reader/outcome binding'
        files={e['path']:e for e in entries if e['root']==root}
        def regular(name):
            assert name in files and files[name]['kind']=='regular', 'fatal witness missing regular '+name
            return export_decoded(files[name]['payload'],EXPORT_LIMITS['file'])
        assert regular('cleanup-complete')==b'quiescent' and regular('fatal-unwind')==b'wait-calls=1;complete=true;retained=true', 'fatal witness drain/publication proof'
        raw=regular('wrapper-trace')
        assert raw.endswith(b'\n') and b'\0' not in raw, 'fatal witness malformed trace'
        text=raw.decode('utf-8');lines=text.splitlines()
        assert lines and re.fullmatch(r'Bash [0-9]+\.[0-9]+\.[0-9]+[^\r\n]*',lines[0]), 'fatal witness trace header'
        assert len(lines)<=65536 and all(len(line)<=65536 for line in lines), 'fatal witness trace bound'
        assert all(line.startswith('+focused: ') for line in lines[1:]), 'fatal witness malformed trace'
        assert set(re.findall(r'dotty-protocol-[0-9]+',text))=={epoch}, 'fatal witness cross-root trace'
        prefix="+focused: printf '%s|%s|%s|%s|%s\\n' "
        event_lines=[line[len(prefix):] for line in lines if line.startswith(prefix)]
        assert event_lines, 'fatal witness missing event trace'
        for line in event_lines:
            fields=line.split(' ',4)
            assert len(fields)==5 and fields[1:4]==['reader',epoch,'absent'], 'fatal witness trace reader/epoch binding'
        assert 'boundary reader '+epoch+' absent ready' in event_lines, 'fatal witness missing boundary trace'
        advance=['+focused: gate_data=','+focused: gate_code=','+focused: IFS=','+focused: read -r gate_data']
        enter=prefix+"read-enter-2 reader "+epoch+" absent 'stage=build;cancelled=0'"
        first=prefix+"read-enter-1 reader "+epoch+" absent 'stage=build;cancelled=0'"
        returned=prefix+"read-return-1 reader "+epoch+" absent 'stage=build;cancelled=0;code=0;class=wake'"
        assert any(lines[i:i+5]==[first]+advance for i in range(len(lines))) and returned in lines, 'fatal witness mandatory first-read trace'
        call="+focused: protocol_event read-enter-2 'stage=build;cancelled=0'"
        assert all(line in (enter,call) for line in lines if re.search(r'read-enter-2(?: |$)',line)), 'fatal witness malformed enter2 trace'
        # A valid complete trace can stop before this sequence on another schedule.
        qualifies=any(lines[i:i+5]==[enter]+advance for i in range(len(lines)))
        if qualifies:
            expected=('phase=reader\n epoch='+epoch+'\n anchor=absent\n action=stage=build;cancelled=0').encode()
            assert regular('read-enter-2')==expected, 'fatal witness read-enter-2 frame'


def _check_export_entry_count(entries):
    assert len(entries) <= EXPORT_LIMITS['entries'], 'export entry limit'


def configure(private_tmp, uid):
    """One already validated fresh CI namespace, not a path alias or log rewrite."""
    global PRIVATE_TMP, PRIVATE_UID, PROTOCOL_ROOT
    assert PRIVATE_TMP is None, 'evidence grammar already bound'
    PRIVATE_TMP = Path(private_tmp)
    PRIVATE_UID = uid
    assert PRIVATE_TMP.is_absolute() and uid > 0
    PROTOCOL_ROOT = re.escape(str(PRIVATE_TMP)) + r'/dotty-protocol-[0-9]+'
    PROTOCOL_CHILD['TestFocusedChildInvocationContext'] = [
        str(PRIVATE_TMP) + '/TestFocusedChildInvocationContextROOT/001/driver_with_spaces',
        './driver_with_spaces',
    ]
