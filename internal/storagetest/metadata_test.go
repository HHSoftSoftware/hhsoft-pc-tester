package storagetest

import (
	"bytes"
	"reflect"
	"testing"
)

func TestGeometryParsingDistinguishesKnownValuesFromMissingOrMalformed(t *testing.T) {
	for _, item := range []struct {
		sectors, block string
		want           Geometry
	}{{"0", "512", Geometry{0, 512}}, {"2048\n", "4096\n", Geometry{1048576, 4096}}, {"2048", "768", Geometry{1048576, 768}}, {"18014398509481984", "512", Geometry{9223372036854775808, 512}}} {
		got, err := ParseGeometry([]byte(item.sectors), []byte(item.block))
		if err != nil || got == nil || *got != item.want {
			t.Fatalf("known geometry: %+v %v", got, err)
		}
	}
	for _, item := range []struct{ sectors, block []byte }{{nil, []byte("512")}, {[]byte("0"), nil}, {[]byte(""), []byte("512")}, {[]byte("unknown"), []byte("512")}, {[]byte("1\n\n"), []byte("512")}, {[]byte("01"), []byte("512")}, {[]byte("-1"), []byte("512")}, {[]byte("18446744073709551615"), []byte("512")}, {[]byte("1"), []byte("4294967296")}, {[]byte("1"), []byte("512 ")}, {bytes.Repeat([]byte{'1'}, 257), []byte("512")}} {
		if got, err := ParseGeometry(item.sectors, item.block); err != ErrAdmission || got != nil {
			t.Fatalf("malformed geometry became known: %+v %v", got, err)
		}
	}
}

func TestAdmissionKnownEmptyAndUnsupportedMediaRemainExcluded(t *testing.T) {
	for _, item := range []struct {
		name, transport, sectors, block string
		want                            []string
	}{
		{"empty optical", "optical", "0", "512", []string{"empty_media", "optical"}},
		{"unsupported sector", "ata", "2048", "768", []string{"unsupported_geometry"}},
		{"signed offset overflow", "ata", "18014398509481984", "512", []string{"unsupported_geometry"}},
	} {
		t.Run(item.name, func(t *testing.T) {
			a := physicalNode("block:a", "8:0")
			a.Transport = item.transport
			geometry, err := ParseGeometry([]byte(item.sectors), []byte(item.block))
			if err != nil {
				t.Fatal(err)
			}
			a.Geometry = geometry
			got, err := admitFromNodes(completeDiscovery(a, physicalNode("block:b", "8:16")), nil, []string{})
			if err != nil || len(got.Targets) != 2 || !reflect.DeepEqual(got.Targets[0].Exclusions, item.want) || len(got.Targets[0].Ranges) != 0 || len(got.Targets[1].Ranges) != 3 {
				t.Fatalf("known exclusion: %+v %v", got, err)
			}
		})
	}
	d := completeDiscovery(physicalNode("block:a", "8:0"), physicalNode("block:b", "8:16"))
	d.Nodes[0].Geometry = nil
	requireAdmissionError(t, d, []string{"block:b"}, []string{})
}

func TestAdmissionReadOnlyMediaAndBlockNameAlias(t *testing.T) {
	d := completeDiscovery(physicalNode("block:a", "8:0"))
	d.Nodes[0].ReadOnly = true
	after, err := admitFromNodes(d, nil, []string{})
	if err != nil || len(after.Targets[0].Ranges) != 3 {
		t.Fatalf("read-only media lost valid reads: %+v %v", after, err)
	}
	d.Nodes[0].ID = "block:a:alias"
	requireAdmissionError(t, d, nil, []string{})
}
