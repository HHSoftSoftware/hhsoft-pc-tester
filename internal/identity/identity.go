// Package identity derives privacy-preserving identities for hardware sets.
package identity

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"sort"
)

var (
	ErrEmptyDeploymentKey = errors.New("identity: deployment key must not be empty")
	ErrMissingDomain      = errors.New("identity: component domain must not be empty")
	ErrMissingIdentifier  = errors.New("identity: missing identifier must be omitted")
)

const (
	componentHMACContext = "HHSoft component v1"
	systemHMACContext    = "HHSoft system v1"
	keyIDHMACContext     = "HHSoft fingerprint key identifier v1"
	portablePartContext  = "HHSoft portable component v1"
	portableSetContext   = "HHSoft portable system v1"
)

// ComponentMaterial holds one normalized identifier. Domain must be a public,
// normalized component type. Components without an identifier must be omitted.
type ComponentMaterial struct {
	Domain   string
	Material []byte
}

type Digest [sha256.Size]byte

// ComponentPseudonym contains no raw identifier.
type ComponentPseudonym struct {
	Domain string
	Digest Digest
}

// DeploymentIdentity is the default, deployment-keyed identity result.
type DeploymentIdentity struct {
	ComponentPseudonyms []ComponentPseudonym
	SystemHMAC          Digest
}

// PortableIdentity is deliberately distinct from deployment pseudonyms.
// Unkeyed identifiers permit cross-deployment correlation and dictionary
// searching, so callers should prefer Compute.
type PortableIdentity struct {
	ComponentDigests []Digest
	SystemDigest     Digest
}

func DeploymentKeyID(deploymentKey []byte) (Digest, error) {
	if len(deploymentKey) == 0 {
		return Digest{}, ErrEmptyDeploymentKey
	}
	mac := hmac.New(sha256.New, deploymentKey)
	writeContext(mac, keyIDHMACContext)
	var result Digest
	copy(result[:], mac.Sum(nil))
	return result, nil
}

// Compute creates typed component HMAC pseudonyms and a system HMAC over only
// their canonical sorted multiset. The result never retains raw identifiers.
func Compute(deploymentKey []byte, components []ComponentMaterial) (DeploymentIdentity, error) {
	if len(deploymentKey) == 0 {
		return DeploymentIdentity{}, ErrEmptyDeploymentKey
	}
	if err := validate(components); err != nil {
		return DeploymentIdentity{}, err
	}

	pseudonyms := make([]ComponentPseudonym, len(components))
	for index, component := range components {
		mac := hmac.New(sha256.New, deploymentKey)
		writeSeparated(mac, componentHMACContext, component.Domain, component.Material)
		pseudonyms[index].Domain = component.Domain
		copy(pseudonyms[index].Digest[:], mac.Sum(nil))
	}
	sortPseudonyms(pseudonyms)

	mac := hmac.New(sha256.New, deploymentKey)
	writeContext(mac, systemHMACContext)
	writePseudonymSet(mac, pseudonyms)
	result := DeploymentIdentity{ComponentPseudonyms: pseudonyms}
	copy(result.SystemHMAC[:], mac.Sum(nil))
	return result, nil
}

// ComputePortable provides the explicitly unkeyed alternative. It remains
// domain-separated from Compute and rejects missing identifiers.
func ComputePortable(components []ComponentMaterial) (PortableIdentity, error) {
	if err := validate(components); err != nil {
		return PortableIdentity{}, err
	}

	digests := make([]Digest, len(components))
	for index, component := range components {
		h := sha256.New()
		writeSeparated(h, portablePartContext, component.Domain, component.Material)
		copy(digests[index][:], h.Sum(nil))
	}
	sort.Slice(digests, func(left, right int) bool {
		return bytes.Compare(digests[left][:], digests[right][:]) < 0
	})

	h := sha256.New()
	writeContext(h, portableSetContext)
	writeDigestSet(h, digests)
	result := PortableIdentity{ComponentDigests: digests}
	copy(result.SystemDigest[:], h.Sum(nil))
	return result, nil
}

func validate(components []ComponentMaterial) error {
	for _, component := range components {
		if component.Domain == "" {
			return ErrMissingDomain
		}
		if len(component.Material) == 0 {
			return ErrMissingIdentifier
		}
	}
	return nil
}

func sortPseudonyms(pseudonyms []ComponentPseudonym) {
	sort.Slice(pseudonyms, func(left, right int) bool {
		if pseudonyms[left].Domain != pseudonyms[right].Domain {
			return pseudonyms[left].Domain < pseudonyms[right].Domain
		}
		return bytes.Compare(pseudonyms[left].Digest[:], pseudonyms[right].Digest[:]) < 0
	})
}

func writeSeparated(h hash.Hash, context, domain string, material []byte) {
	writeContext(h, context)
	_, _ = h.Write([]byte(domain))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(material)
}

func writeContext(h hash.Hash, context string) {
	_, _ = h.Write([]byte(context))
	_, _ = h.Write([]byte{0})
}

func writePseudonymSet(h hash.Hash, pseudonyms []ComponentPseudonym) {
	writeUint64(h, uint64(len(pseudonyms)))
	for _, pseudonym := range pseudonyms {
		writeField(h, []byte(pseudonym.Domain))
		writeField(h, pseudonym.Digest[:])
	}
}

func writeDigestSet(h hash.Hash, digests []Digest) {
	writeUint64(h, uint64(len(digests)))
	for _, digest := range digests {
		writeField(h, digest[:])
	}
}

func writeField(h hash.Hash, value []byte) {
	writeUint64(h, uint64(len(value)))
	_, _ = h.Write(value)
}

func writeUint64(h hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = h.Write(encoded[:])
}
