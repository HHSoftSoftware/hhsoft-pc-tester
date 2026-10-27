package storage

import (
	"strconv"
	"strings"
)

type deviceNumber struct {
	major uint32
	minor uint32
}

// parseDeviceNumber accepts the canonical mountinfo/sysfs identity. The sysfs
// reader, not this parser, handles the kernel's optional single trailing LF.
func parseDeviceNumber(value string) (deviceNumber, bool) {
	separator := strings.IndexByte(value, ':')
	if separator <= 0 || separator == len(value)-1 || len(value) > 21 {
		return deviceNumber{}, false
	}
	parse := func(part string) (uint32, bool) {
		if len(part) > 10 || len(part) > 1 && part[0] == '0' {
			return 0, false
		}
		for index := range part {
			if part[index] < '0' || part[index] > '9' {
				return 0, false
			}
		}
		number, err := strconv.ParseUint(part, 10, 32)
		return uint32(number), err == nil
	}
	major, majorOK := parse(value[:separator])
	minor, minorOK := parse(value[separator+1:])
	return deviceNumber{major: major, minor: minor}, majorOK && minorOK
}

func (number deviceNumber) String() string {
	return strconv.FormatUint(uint64(number.major), 10) + ":" + strconv.FormatUint(uint64(number.minor), 10)
}
