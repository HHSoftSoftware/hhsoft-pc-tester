package storageevidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

func TestStorageResultStrictJSONRequiredNullableAndPrivateFreeShape(t *testing.T) {
	raw, err := json.Marshal(literalPass())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, old, new string }{
		{"unknown media", "\"schema\":", "\"media_content\":\"secret\",\"schema\":"},
		{"content fingerprint", "\"schema\":", "\"content_sha256\":\"a\",\"schema\":"},
		{"duplicate", "\"policy\":", "\"scope\":\"planned\",\"policy\":"},
		{"escaped duplicate", "\"policy\":", "\"sco\\u0070e\":\"planned\",\"policy\":"},
		{"wrong case", "\"schema\":", "\"Schema\":"},
		{"missing required", "\"media_writes\":false,", ""},
		{"null top array", "\"component_ids\":[]", "\"component_ids\":null"},
		{"null nested array", "\"physical_children\":[]", "\"physical_children\":null"},
		{"null geometry", "\"geometry\":{\"capacity_bytes\":512,\"logical_block_bytes\":512}", "\"geometry\":null"},
		{"missing nullable", ",\"loop_backing\":null", ""},
		{"base64 masks", "\"node_fixture_masks\":[0]", "\"node_fixture_masks\":\"AA==\""},
		{"null masks", "\"node_fixture_masks\":[0]", "\"node_fixture_masks\":null"},
		{"mask overflow", "\"node_fixture_masks\":[0]", "\"node_fixture_masks\":[256]"},
		{"mask numeric alias", "\"node_fixture_masks\":[0]", "\"node_fixture_masks\":[0e0]"},
		{"mask case alias", "\"node_fixture_masks\":[0]", "\"Node_fixture_masks\":[0]"},
		{"unknown node field", "\"read_only\":false", "\"read_only\":false,\"inode\":123"},
		{"number exponent", "\"read_count\":2", "\"read_count\":2e0"},
		{"number bool", "\"read_count\":2", "\"read_count\":true"},
		{"negative unsigned", "\"read_bytes\":1024", "\"read_bytes\":-1"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			changed := bytes.Replace(raw, []byte(item.old), []byte(item.new), 1)
			if bytes.Equal(changed, raw) {
				t.Fatalf("test trigger absent: %s", item.old)
			}
			got, err := Decode(changed)
			if err != ErrInvalid || !reflect.DeepEqual(got, storagetest.Result{}) {
				t.Fatalf("wire forgery accepted: %v", err)
			}
		})
	}
	for _, changed := range [][]byte{append(append([]byte{}, raw...), []byte(" {}")...), append([]byte{0xff}, raw...), bytes.Repeat([]byte{' '}, storagetest.MaximumResultBytes+1)} {
		if _, err := Decode(changed); err != ErrInvalid {
			t.Fatal("invalid encoding or bound admitted")
		}
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, raw, "", "  ") != nil {
		t.Fatal("fixture indent")
	}
	if got, err := Decode(pretty.Bytes()); err != nil || !reflect.DeepEqual(got, literalPass()) {
		t.Fatal("legitimate whitespace lost")
	}
}

func TestStorageFixtureMasksUseRequiredIntegerArrays(t *testing.T) {
	for _, item := range []struct {
		values storagetest.FixtureMasks
		want   string
	}{{storagetest.FixtureMasks{}, "[]"}, {storagetest.FixtureMasks{0, 1, 128, 255}, "[0,1,128,255]"}, {nil, "null"}} {
		raw, err := json.Marshal(item.values)
		if err != nil || string(raw) != item.want {
			t.Fatalf("mask wire %s %v", raw, err)
		}
	}
	r := literalPass()
	r.Snapshot.NodeFixtureMasks = nil
	rehash(t, &r)
	requireInvalidResult(t, r)
	r = literalPass()
	r.Snapshot.Discovery.Nodes = []storagetest.Node{}
	r.Snapshot.NodeFixtureMasks = storagetest.FixtureMasks{}
	r.Devices = []storagetest.DeviceResult{}
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Status = "inconclusive"
	rehash(t, &r)
	raw, err := CanonicalTypedBytes(r)
	if err != nil || !bytes.Contains(raw, []byte(`"node_fixture_masks":[]`)) {
		t.Fatalf("empty mask wire: %s %v", raw, err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CanonicalTypedBytes(decoded)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("typed mask round trip changed")
	}
}

func TestStorageResultMaximumDevicesAndRunReadBudget(t *testing.T) {
	r := literalPass()
	r.Snapshot.Discovery.Nodes = []storagetest.Node{}
	r.Snapshot.NodeFixtureMasks = []uint8{}
	r.Devices = []storagetest.DeviceResult{}
	r.DurationMilliseconds = 256
	r.AcceptedReadBytes = 100663296
	r.AcceptedComparedBytes = 50331648
	r.AcceptedMatchedRanges = 768
	for i := 0; i < 256; i++ {
		base := literalPass()
		name := fmt.Sprintf("d%03d", i)
		node := base.Snapshot.Discovery.Nodes[0]
		node.ID = "block:" + name
		node.Number = fmt.Sprintf("8:%d", i)
		node.Transport = "virtio"
		node.Geometry = &storagetest.Geometry{CapacityBytes: 196608, LogicalBlockBytes: 512}
		r.Snapshot.Discovery.Nodes = append(r.Snapshot.Discovery.Nodes, node)
		r.Snapshot.NodeFixtureMasks = append(r.Snapshot.NodeFixtureMasks, 0)
		device := base.Devices[0]
		device.Interface = name
		device.StartedMilliseconds = int64(i)
		device.FinishedMilliseconds = int64(i + 1)
		device.Observation.Interface = name
		device.Observation.Identity = storagetest.Identity{Major: 8, Minor: uint32(i), CapacityBytes: 196608, LogicalBlockBytes: 512}
		device.Observation.Ranges = []storagetest.RangeObservation{{Offset: 0, Length: 65536, ReadCount: 2, FirstBytes: 65536, SecondBytes: 65536, Outcome: "matched"}, {Offset: 65536, Length: 65536, ReadCount: 2, FirstBytes: 65536, SecondBytes: 65536, Outcome: "matched"}, {Offset: 131072, Length: 65536, ReadCount: 2, FirstBytes: 65536, SecondBytes: 65536, Outcome: "matched"}}
		device.Observation.ReadBytes = 393216
		device.Observation.ComparedBytes = 196608
		device.Observation.MatchedRanges = 3
		r.Devices = append(r.Devices, device)
	}
	rehash(t, &r)
	raw, err := CanonicalTypedBytes(r)
	if err != nil || len(raw)+1 > 524288 {
		t.Fatalf("maximum literal model cannot serialize: %d %v", len(raw), err)
	}
	if _, err := Decode(append(raw, '\n')); err != nil {
		t.Fatalf("maximum decode: %v", err)
	}
	view, err := Summary(r)
	if err != nil || len(view.Devices) != 256 || view.AcceptedReadBytes != 100663296 || view.AcceptedComparedBytes != 50331648 || view.AcceptedMatchedRanges != 768 {
		t.Fatalf("maximum counters: %+v %v", view, err)
	}
	r.AcceptedReadBytes++
	requireInvalidResult(t, r)
	r.AcceptedReadBytes--
	r.Devices = append(r.Devices, r.Devices[255])
	requireInvalidResult(t, r)
}
