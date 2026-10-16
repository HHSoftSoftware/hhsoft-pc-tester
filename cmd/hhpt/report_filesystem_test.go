package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReportFilesystemRecognizesOnlyExplicitAllowedHeaders(t *testing.T) {
	for _, filesystem := range []string{"vfat", "exfat", "ext4"} {
		t.Run(filesystem, func(t *testing.T) {
			got, err := probeReportFilesystem(bytes.NewReader(reportFilesystemFixture(filesystem)))
			if err != nil || got != filesystem {
				t.Fatalf("filesystem = %q, error = %v", got, err)
			}
		})
	}
	for _, test := range []struct {
		name string
		make func() []byte
	}{
		{"unknown", func() []byte { return make([]byte, 2048) }},
		{"truncated", func() []byte { return reportFilesystemFixture("vfat")[:512] }},
		{"ntfs", func() []byte { data := reportFilesystemFixture("exfat"); copy(data[3:11], "NTFS    "); return data }},
		{"bad FAT signature", func() []byte { data := reportFilesystemFixture("vfat"); data[510] = 0; return data }},
		{"bad FAT sector size", func() []byte { data := reportFilesystemFixture("vfat"); data[11] = 3; return data }},
		{"bad exFAT reserved bytes", func() []byte { data := reportFilesystemFixture("exfat"); data[11] = 1; return data }},
		{"ext2 or ext3", func() []byte { data := reportFilesystemFixture("ext4"); data[1120] = 0; return data }},
		{"external journal device", func() []byte { data := reportFilesystemFixture("ext4"); data[1252] = 1; return data }},
		{"external journal UUID", func() []byte { data := reportFilesystemFixture("ext4"); data[1232] = 1; return data }},
		{"external journal superblock", func() []byte { data := reportFilesystemFixture("ext4"); data[1120] |= 8; return data }},
		{"missing internal journal", func() []byte { data := reportFilesystemFixture("ext4"); data[1116] = 4; return data }},
		{"stored mount options", func() []byte {
			data := reportFilesystemFixture("ext4")
			copy(data[1536:], "journal_dev=8:1")
			return data
		}},
		{"ambiguous FAT and ext4", func() []byte {
			data := reportFilesystemFixture("vfat")
			copy(data[1024:], reportFilesystemFixture("ext4")[1024:])
			return data
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if filesystem, err := probeReportFilesystem(bytes.NewReader(test.make())); err == nil {
				t.Fatalf("unsafe or unknown header accepted as %s", filesystem)
			}
		})
	}
}

func TestReportFilesystemProbeIsBoundedAndPropagatesReadFailure(t *testing.T) {
	reader := &reportProbeReader{data: reportFilesystemFixture("ext4")}
	if _, err := probeReportFilesystem(reader); err != nil || reader.calls != 1 {
		t.Fatalf("bounded probe failed: calls=%d error=%v", reader.calls, err)
	}
	reader.failure = errors.New("injected read failure")
	if _, err := probeReportFilesystem(reader); err == nil {
		t.Fatal("failed report-media read was accepted")
	}
}

type reportProbeReader struct {
	data    []byte
	calls   int
	failure error
}

func (reader *reportProbeReader) ReadAt(destination []byte, offset int64) (int, error) {
	reader.calls++
	if offset != 0 || len(destination) > 2048 {
		return 0, errors.New("probe read outside the allowed header")
	}
	if reader.failure != nil {
		return 0, reader.failure
	}
	return bytes.NewReader(reader.data).ReadAt(destination, offset)
}

var _ io.ReaderAt = (*reportProbeReader)(nil)

// Fixed on-disk offsets from the published filesystem layouts; these fixtures
// contain no identity, files, real device data, or filesystem formatting code.
func reportFilesystemFixture(filesystem string) []byte {
	data := make([]byte, 2048)
	switch filesystem {
	case "vfat":
		data[12] = 2 // 512-byte sectors
		data[13], data[14], data[16], data[22] = 8, 1, 2, 1
		copy(data[54:62], "FAT16   ")
		data[510], data[511] = 0x55, 0xaa
	case "exfat":
		copy(data[3:11], "EXFAT   ")
		data[108], data[109], data[110] = 9, 3, 1
		data[510], data[511] = 0x55, 0xaa
	case "ext4":
		data[1080], data[1081] = 0x53, 0xef
		data[1120] = 0x40 // extents
	}
	return data
}
