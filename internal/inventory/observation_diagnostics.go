package inventory

import (
	"io/fs"
	"os"
	"syscall"
)

// CollectionDiagnostics counts failed optional file observations in one
// successful collection. It contains no paths, provider text or device identity.
// Clean absence and completed empty reads do not increment these counters.
// Counts describe attempts, not distinct devices, and are not snapshot fields.
type CollectionDiagnostics struct {
	ScalarFailures uint32
	CPUFailures    uint32
	MemoryFailures uint32
	InputFailures  uint32
}

func (diagnostics CollectionDiagnostics) total() uint64 {
	return uint64(diagnostics.ScalarFailures) + uint64(diagnostics.CPUFailures) +
		uint64(diagnostics.MemoryFailures) + uint64(diagnostics.InputFailures)
}

// Valid checks the aggregate limit shared by all file attempts in one pass.
func (diagnostics CollectionDiagnostics) Valid() bool {
	return diagnostics.total() <= maxInventoryFileAttempts
}

// HasFailures reports whether this successful collection lost an observation.
func (diagnostics CollectionDiagnostics) HasFailures() bool {
	return diagnostics.total() != 0
}

func (budget *inventoryAcquisitionBudget) recordObservationFailure(limit int) error {
	if !budget.valid() || budget.diagnostics.total() == maxInventoryFileAttempts {
		return ErrAcquisitionBudget
	}
	switch limit {
	case inventoryScalarLimit:
		budget.diagnostics.ScalarFailures++
	case inventoryCPULimit:
		budget.diagnostics.CPUFailures++
	case inventoryMemoryLimit:
		budget.diagnostics.MemoryFailures++
	case inventoryInputLimit:
		budget.diagnostics.InputFailures++
	default:
		return errInventoryFileRead
	}
	return nil
}

// Only known open wrappers and concrete standard-library absence values count
// as clean absence. Opaque errors remain failed observations; never call their
// Error, Is or Unwrap methods. The bound also rejects cyclic wrapper chains.
func knownObservationAbsence(err error) bool {
	for depth := 0; depth < 32; depth++ {
		if err == fs.ErrNotExist {
			return true
		}
		switch value := err.(type) {
		case *fs.PathError:
			if value == nil || value.Op != "open" {
				return false
			}
			err = value.Err
		case syscall.Errno:
			// This concrete standard-library value cannot dispatch a provider's
			// Error, Is or Unwrap callback.
			return os.IsNotExist(value)
		default:
			return false
		}
	}
	return false
}
