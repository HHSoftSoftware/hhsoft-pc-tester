package storagetest

// Node contains public kernel topology and geometry, never serials or media data.
type Node struct {
	ID               string       `json:"id"`
	Number           string       `json:"number"`
	Kind             string       `json:"kind"`
	Parent           string       `json:"parent"`
	Transport        string       `json:"transport"`
	Geometry         *Geometry    `json:"geometry"`
	ReadOnly         bool         `json:"read_only"`
	Removable        bool         `json:"removable"`
	USBAncestors     []string     `json:"usb_ancestors"`
	PhysicalChildren []string     `json:"physical_children"`
	Holders          []string     `json:"holders"`
	Slaves           []string     `json:"slaves"`
	LoopState        string       `json:"loop_state"`
	LoopBacking      *LoopBacking `json:"loop_backing"`
}

// Geometry is present only after complete mandatory numeric acquisition.
// Nil means unavailable, while a present zero capacity is a known empty value.
type Geometry struct {
	CapacityBytes     uint64 `json:"capacity_bytes"`
	LogicalBlockBytes uint32 `json:"logical_block_bytes"`
}

type FixtureObservation struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	DeviceNumber string `json:"device_number"`
}

// LoopBacking binds the allowed numeric kernel tuple without exporting its
// inode or path. BindingSHA256 is the domain-separated tuple digest specified
// by the loop contract; syntactic validation is not production ABI proof.
type LoopBacking struct {
	Kind          string `json:"kind"`
	Device        string `json:"device"`
	BindingSHA256 string `json:"binding_sha256"`
}

// MemoryMount preserves the filesystem fact for an exact major-zero number.
// Its producer must reject conflicting aliases before deduplicating mount rows.
type MemoryMount struct {
	Number     string `json:"number"`
	Filesystem string `json:"filesystem"`
}

// Discovery is metadata, never authority to open a pathname. Production
// construction remains gated on reviewed kernel backing and lifetime proof.
// Number lists come from the same complete, strict mount projection.
type Discovery struct {
	Complete           bool          `json:"complete"`
	BackingComplete    bool          `json:"backing_complete"`
	SwapState          string        `json:"swap_state"`
	Nodes              []Node        `json:"nodes"`
	MountedNumbers     []string      `json:"mounted_numbers"`
	MemoryMounts       []MemoryMount `json:"memory_mounts"`
	BootBackingNumbers []string      `json:"boot_backing_numbers"`
}

type Target struct {
	ID                string   `json:"id"`
	Number            string   `json:"number"`
	CapacityBytes     uint64   `json:"capacity_bytes"`
	LogicalBlockBytes uint32   `json:"logical_block_bytes"`
	Exclusions        []string `json:"exclusions"`
	Ranges            []Range  `json:"ranges"`
	FixtureMask       uint8    `json:"fixture_mask"`
}

type Admission struct {
	RequestedIDs        []string             `json:"requested_ids"`
	FixtureIDs          []string             `json:"fixture_ids"`
	FixtureObservations []FixtureObservation `json:"fixture_observations"`
	ResolvedFixtureMask uint8                `json:"resolved_fixture_mask"`
	NodeFixtureMasks    []uint8              `json:"node_fixture_masks"`
	Targets             []Target             `json:"targets"`
}
