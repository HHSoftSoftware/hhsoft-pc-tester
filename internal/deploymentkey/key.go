package deploymentkey

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/identity"
)

const (
	Filename           = "hhpt-deployment-key.v1"
	MaximumEncodedSize = 138
	magic              = "HHPTDK1\n"
	maximumEpochLength = 64
	checksumContext    = "HHSoft deployment fingerprint key file v1"
)

var ErrInvalid = errors.New("deployment key: invalid or corrupt file")

type Record struct {
	Epoch string
	Key   [32]byte
}

func New(epoch string, random io.Reader) (Record, error) {
	if !ValidEpoch(epoch) {
		return Record{}, errors.New("deployment key: epoch must use 1-64 letters, digits, dots, underscores, or hyphens")
	}
	record := Record{Epoch: epoch}
	if _, err := io.ReadFull(random, record.Key[:]); err != nil {
		return Record{}, err
	}
	return record, nil
}

func Encode(record Record) ([]byte, error) {
	if !ValidEpoch(record.Epoch) {
		return nil, ErrInvalid
	}
	var body bytes.Buffer
	body.WriteString(magic)
	_ = binary.Write(&body, binary.BigEndian, uint16(len(record.Epoch)))
	body.WriteString(record.Epoch)
	body.Write(record.Key[:])
	digest := fileDigest(body.Bytes())
	body.Write(digest[:])
	return body.Bytes(), nil
}

func Decode(encoded []byte) (Record, error) {
	if len(encoded) < len(magic)+2+1+len(Record{}.Key)+sha256.Size || len(encoded) > MaximumEncodedSize || string(encoded[:len(magic)]) != magic {
		return Record{}, ErrInvalid
	}
	epochLength := int(binary.BigEndian.Uint16(encoded[len(magic) : len(magic)+2]))
	expected := len(magic) + 2 + epochLength + len(Record{}.Key) + sha256.Size
	if epochLength < 1 || epochLength > maximumEpochLength || len(encoded) != expected {
		return Record{}, ErrInvalid
	}
	bodyEnd := expected - sha256.Size
	wantDigest := fileDigest(encoded[:bodyEnd])
	if !hmac.Equal(encoded[bodyEnd:], wantDigest[:]) {
		return Record{}, ErrInvalid
	}
	record := Record{Epoch: string(encoded[len(magic)+2 : len(magic)+2+epochLength])}
	if !ValidEpoch(record.Epoch) {
		return Record{}, ErrInvalid
	}
	copy(record.Key[:], encoded[len(magic)+2+epochLength:bodyEnd])
	return record, nil
}

func KeyID(record Record) (string, error) {
	digest, err := identity.DeploymentKeyID(record.Key[:])
	if err != nil {
		return "", err
	}
	return "hmac-sha256:" + hex.EncodeToString(digest[:]), nil
}

func ValidEpoch(epoch string) bool {
	if len(epoch) < 1 || len(epoch) > maximumEpochLength || epoch == "ephemeral-session" {
		return false
	}
	for index, character := range epoch {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9' && index > 0) || (index > 0 && strings.ContainsRune("._-", character)) {
			continue
		}
		return false
	}
	return true
}

func fileDigest(body []byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(checksumContext))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(body)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}
