package usbtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

type usbAbsenceImpostor struct{}

func (usbAbsenceImpostor) Error() string { return "synthetic missing" }
func (usbAbsenceImpostor) Is(error) bool { return true }

type usbAbsenceCycle struct{}

func (*usbAbsenceCycle) Error() string   { return "synthetic cycle" }
func (e *usbAbsenceCycle) Unwrap() error { return e }

func TestUSBOptionalAbsenceRequiresSoleOpenCause(t *testing.T) {
	var nilPath *os.PathError
	for _, test := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"sentinel", os.ErrNotExist, true},
		{"errno", syscall.ENOENT, true},
		{"open path", &os.PathError{Op: "open", Path: "owned", Err: os.ErrNotExist}, true},
		{"wrapped open", fmt.Errorf("owned wrapper: %w", &os.PathError{Op: "open", Err: os.ErrNotExist}), true},
		{"permission", os.ErrPermission, false},
		{"read absence", &os.PathError{Op: "read", Err: os.ErrNotExist}, false},
		{"close absence", &os.PathError{Op: "close", Err: os.ErrNotExist}, false},
		{"joined absence", errors.Join(os.ErrNotExist), false},
		{"mixed failure", errors.Join(os.ErrNotExist, os.ErrPermission), false},
		{"impostor", usbAbsenceImpostor{}, false},
		{"cycle", &usbAbsenceCycle{}, false},
		{"typed nil", nilPath, false},
		{"no cause", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			open := func(string) (usbMetadataFile, error) { return nil, test.err }
			data, err := acquireUSBMetadata(t.Context(), "owned", open)
			want := errUSBMetadataRead
			if test.absent {
				want = errUSBMetadataAbsent
			}
			if data != nil || err != want {
				t.Fatal("absence cause was not distinguished from a hard failure")
			}
			if data, err := readUSBMetadata(t.Context(), "owned", open); data != nil || err != errUSBMetadataRead {
				t.Fatal("mandatory observation accepted absence")
			}
		})
	}
}

func TestUSBOptionalAbsenceCannotHideOwnershipOrCancellation(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, closeFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("owned_%t_close_%t", owned, closeFailure), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				file := usbMetadata("ignored")
				if closeFailure {
					file.closeErr = os.ErrPermission
				}
				data, err := acquireUSBMetadata(ctx, "owned", func(string) (usbMetadataFile, error) {
					cancel()
					if owned {
						return file, os.ErrNotExist
					}
					return nil, os.ErrNotExist
				})
				want := error(context.Canceled)
				if owned {
					want = errUSBMetadataRead
				}
				if owned && closeFailure {
					want = errUSBMetadataClose
				}
				wantCloses := 0
				if owned {
					wantCloses = 1
				}
				if data != nil || err != want || file.closes != wantCloses || file.reads != 0 {
					t.Fatal("absence concealed a live handle, hard error, or cancellation")
				}
			})
		}
	}
}
