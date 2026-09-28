package releaseverify

import (
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// NewWithoutSCTPolicy builds a verifier identical to New except that the
// Fulcio SCT threshold is dropped.
//
// It exists only because sigstore-go's in-process virtual Sigstore mints
// Fulcio leaf certificates without an embedded Signed Certificate Timestamp,
// so the end-to-end fixtures cannot satisfy the production policy. Everything
// else — the pinned identity, the transparency-log and observer-timestamp
// thresholds, and every manifest/checksum/provenance binding — stays exactly
// as production runs it.
//
// The SCT requirement itself is covered separately by asserting that the
// production constructor REJECTS those same fixtures. This symbol lives in an
// _test.go file, so no non-test build can reach it.
func NewWithoutSCTPolicy(trust root.TrustedMaterial, repository string) (*Verifier, error) {
	return newVerifier(trust, repository,
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
}
