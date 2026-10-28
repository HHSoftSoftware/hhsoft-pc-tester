package storagetest

import (
	"math"
	"strconv"
)

// ParseGeometry parses complete bounded sysfs scalar values. The caller must
// already have proved EOF and successful closure; this function performs no I/O.
// Unsupported known arithmetic remains representable for explicit exclusion.
func ParseGeometry(sectors, logicalBlock []byte) (*Geometry, error) {
	parse := func(raw []byte, bits int) (uint64, error) {
		if len(raw) == 0 || len(raw) > 256 {
			return 0, ErrAdmission
		}
		if raw[len(raw)-1] == '\n' {
			raw = raw[:len(raw)-1]
		}
		if len(raw) == 0 || len(raw) > 20 || len(raw) > 1 && raw[0] == '0' {
			return 0, ErrAdmission
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				return 0, ErrAdmission
			}
		}
		value, err := strconv.ParseUint(string(raw), 10, bits)
		if err != nil {
			return 0, ErrAdmission
		}
		return value, nil
	}
	count, err := parse(sectors, 64)
	if err != nil || count > math.MaxUint64/512 {
		return nil, ErrAdmission
	}
	block, err := parse(logicalBlock, 32)
	if err != nil {
		return nil, ErrAdmission
	}
	return &Geometry{CapacityBytes: count * 512, LogicalBlockBytes: uint32(block)}, nil
}
