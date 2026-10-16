package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

const reportFilesystemHeaderBytes = 2048

// Probe only the authorized report partition. This is type recognition, not a
// filesystem integrity check; the kernel must still accept the explicit type.
// Layout references:
// https://docs.kernel.org/filesystems/ext4/super.html
// https://learn.microsoft.com/en-us/windows/win32/fileio/exfat-specification
// https://github.com/util-linux/util-linux/blob/master/libblkid/src/superblocks/vfat.c
func probeReportFilesystem(source io.ReaderAt) (string, error) {
	header := make([]byte, reportFilesystemHeaderBytes)
	if count, err := source.ReadAt(header, 0); err != nil || count != len(header) {
		return "", errors.New("cannot read the authorized report filesystem header")
	}
	var matches []string
	if recognizedReportFAT(header) {
		matches = append(matches, "vfat")
	}
	if bytes.Equal(header[3:11], []byte("EXFAT   ")) &&
		bytes.Equal(header[11:64], make([]byte, 53)) && header[510] == 0x55 && header[511] == 0xaa &&
		header[108] >= 9 && header[108] <= 12 && int(header[108])+int(header[109]) <= 25 &&
		(header[110] == 1 || header[110] == 2) {
		matches = append(matches, "exfat")
	}
	super := header[1024:]
	if binary.LittleEndian.Uint16(super[0x38:]) == 0xef53 {
		// Require the ext4 extents feature. External journals and stored mount
		// options can direct I/O outside this partition, so reject them entirely.
		features := binary.LittleEndian.Uint32(super[0x60:])
		hasJournal := binary.LittleEndian.Uint32(super[0x5c:])&0x4 != 0
		if features&0x40 == 0 || features&0x8 != 0 ||
			binary.LittleEndian.Uint32(super[0xe4:]) != 0 ||
			!bytes.Equal(super[0xd0:0xe0], make([]byte, 16)) ||
			!bytes.Equal(super[0x200:0x240], make([]byte, 64)) ||
			(hasJournal && binary.LittleEndian.Uint32(super[0xe0:]) == 0) {
			return "", errors.New("report ext4 filesystem uses unsupported features or external mount configuration")
		}
		matches = append(matches, "ext4")
	}
	if len(matches) != 1 {
		return "", errors.New("report filesystem must unambiguously identify as vfat, exfat, or ext4")
	}
	return matches[0], nil
}

func recognizedReportFAT(header []byte) bool {
	if header[510] != 0x55 || header[511] != 0xaa {
		return false
	}
	sectorSize := binary.LittleEndian.Uint16(header[11:13])
	clusterSize := header[13]
	if sectorSize < 512 || sectorSize > 4096 || sectorSize&(sectorSize-1) != 0 ||
		clusterSize == 0 || clusterSize&(clusterSize-1) != 0 ||
		binary.LittleEndian.Uint16(header[14:16]) == 0 || header[16] == 0 {
		return false
	}
	legacyType := string(header[54:62])
	legacyFAT := binary.LittleEndian.Uint16(header[22:24]) != 0 &&
		(legacyType == "FAT12   " || legacyType == "FAT16   " || legacyType == "FAT     ")
	fat32 := binary.LittleEndian.Uint16(header[22:24]) == 0 &&
		binary.LittleEndian.Uint32(header[36:40]) != 0 && string(header[82:90]) == "FAT32   "
	return legacyFAT != fat32
}
