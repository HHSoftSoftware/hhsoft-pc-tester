package storageevidence

import (
	"errors"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

func finalBlock() inventory.Component {
	return inventory.Component{Type: inventory.ComponentBlock, ID: "block:a", Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:0"}, {Name: "size_sectors", Value: "1"}, {Name: "logical_block_size", Value: "512"}}}
}

func TestStorageInventoryRequiresExactPassedIdentityEvenOnLaterError(t *testing.T) {
	for _, partial := range []bool{false, true} {
		r := literalPass()
		if partial {
			r.Status = "error"
			r.StopReason = "error"
			r.Error = "safety_changed"
		}
		if err := ValidateInventoryBinding(r, []inventory.Component{finalBlock()}); err != nil {
			t.Fatalf("matching final identity: %v", err)
		}
		for _, item := range []struct {
			name string
			rows func() []inventory.Component
		}{
			{"missing", func() []inventory.Component { return []inventory.Component{} }},
			{"duplicate component", func() []inventory.Component { return []inventory.Component{finalBlock(), finalBlock()} }},
			{"wrong type", func() []inventory.Component {
				row := finalBlock()
				row.Type = inventory.ComponentUSB
				return []inventory.Component{row}
			}},
			{"wrong ID", func() []inventory.Component {
				row := finalBlock()
				row.ID = "block:b"
				return []inventory.Component{row}
			}},
			{"missing attribute", func() []inventory.Component {
				row := finalBlock()
				row.Attributes = row.Attributes[:2]
				return []inventory.Component{row}
			}},
			{"duplicate same attribute", func() []inventory.Component {
				row := finalBlock()
				row.Attributes = append(row.Attributes, row.Attributes[0])
				return []inventory.Component{row}
			}},
			{"device replacement", func() []inventory.Component {
				row := finalBlock()
				row.Attributes[0].Value = "8:1"
				return []inventory.Component{row}
			}},
			{"capacity replacement", func() []inventory.Component {
				row := finalBlock()
				row.Attributes[1].Value = "2"
				return []inventory.Component{row}
			}},
			{"sector replacement", func() []inventory.Component {
				row := finalBlock()
				row.Attributes[2].Value = "4096"
				return []inventory.Component{row}
			}},
			{"size overflow", func() []inventory.Component {
				row := finalBlock()
				row.Attributes[1].Value = "18446744073709551615"
				return []inventory.Component{row}
			}},
			{"size alias", func() []inventory.Component {
				row := finalBlock()
				row.Attributes[1].Value = "01"
				return []inventory.Component{row}
			}},
			{"trailing LF", func() []inventory.Component {
				row := finalBlock()
				row.Attributes[1].Value = "1\n"
				return []inventory.Component{row}
			}},
		} {
			t.Run(item.name, func(t *testing.T) {
				if err := ValidateInventoryBinding(r, item.rows()); err != ErrInvalid {
					t.Fatalf("final identity forgery accepted: %v", err)
				}
			})
		}
	}
}

func TestStorageInventoryRetainsVanishedDiagnosticPassAndCleanupError(t *testing.T) {
	r := literalPass()
	r.Devices[0].Accepted = false
	r.Devices[0].Status = "error"
	r.Devices[0].Error = "identity_changed"
	r.Devices[0].FinalBindingConfirmed = false
	r.Status = "error"
	r.StopReason = "error"
	r.Error = "identity_changed"
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	if err := ValidateInventoryBinding(r, []inventory.Component{finalBlock()}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInventoryBinding(r, nil); err != nil {
		t.Fatalf("honest vanished diagnostic was discarded: %v", err)
	}
	view, err := Summary(r)
	if err != nil || view.AcceptedReadBytes != 0 || view.DiagnosticReadBytes != 1024 || view.AcceptedComparedBytes != 0 {
		t.Fatal("vanished diagnostic granted accepted work")
	}
	passed := literalPass()
	passed.Status = "error"
	passed.StopReason = "error"
	passed.Error = "identity_changed"
	if err := ValidateInventoryBinding(passed, nil); err != ErrInvalid {
		t.Fatal("accepted passed device orphan survived final binding")
	}
	r.CleanupConfirmed = false
	r.Status = "error"
	r.StopReason = "cleanup_unconfirmed"
	r.Error = "cleanup_unconfirmed"
	if err := ValidateInventoryBinding(r, []inventory.Component{finalBlock()}); !errors.Is(err, workerexec.ErrCleanupUncertain) {
		t.Fatalf("cleanup uncertainty downgraded: %v", err)
	}
}

func TestStorageInventoryMixedAcceptedPassAndVanishedDiagnostic(t *testing.T) {
	r := literalPass()
	b := literalPass()
	node := b.Snapshot.Discovery.Nodes[0]
	node.ID = "block:b"
	node.Number = "8:1"
	r.Snapshot.Discovery.Nodes = append(r.Snapshot.Discovery.Nodes, node)
	r.Snapshot.NodeFixtureMasks = append(r.Snapshot.NodeFixtureMasks, 0)
	device := b.Devices[0]
	device.Interface = "b"
	device.Accepted = false
	device.Status = "error"
	device.Error = "identity_changed"
	device.FinalBindingConfirmed = false
	device.StartedMilliseconds = 10
	device.FinishedMilliseconds = 18
	device.Observation.Interface = "b"
	device.Observation.Identity.Minor = 1
	r.Devices = append(r.Devices, device)
	r.Status = "error"
	r.StopReason = "error"
	r.Error = "identity_changed"
	r.DurationMilliseconds = 20
	rehash(t, &r)
	if err := ValidateInventoryBinding(r, []inventory.Component{finalBlock()}); err != nil {
		t.Fatalf("vanished diagnostic blocked retained valid pass: %v", err)
	}
	view, err := Summary(r)
	if err != nil || view.AcceptedReadBytes != 1024 || view.AcceptedComparedBytes != 512 || view.DiagnosticReadBytes != 1024 || view.DiagnosticComparedBytes != 512 {
		t.Fatalf("mixed credit: %+v %v", view, err)
	}
	if err := ValidateInventoryBinding(r, nil); err != ErrInvalid {
		t.Fatal("retained passed A disappeared")
	}
	changed := finalBlock()
	changed.Attributes[0].Value = "8:2"
	if err := ValidateInventoryBinding(r, []inventory.Component{changed}); err != ErrInvalid {
		t.Fatal("retained passed A replaced")
	}
	changed = finalBlock()
	changed.ID = "block:b"
	changed.Attributes[0].Value = "8:1"
	if err := ValidateInventoryBinding(r, []inventory.Component{changed}); err != ErrInvalid {
		t.Fatal("diagnostic B substituted for passed A")
	}
}
