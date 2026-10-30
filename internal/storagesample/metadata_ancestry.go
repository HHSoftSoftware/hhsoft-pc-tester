package storagesample

import (
	"context"
	"io"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Link observations do not freeze a symlink, its parents, a mount or a backing
// device. Equal observations cannot exclude a transient change-and-restore.
type storageMetadataLinkFact struct {
	path, target string
	info         os.FileInfo
	absent       bool
}

type storageMetadataAncestryPlan struct {
	chains map[string][]string
	paths  []string
	links  map[string]storageMetadataLinkFact
}

type storageMetadataAncestry struct {
	owners      *[]io.Closer
	initialized bool
	plan        storageMetadataAncestryPlan
	anchors     map[string]storageMetadataAnchor
	links       map[string]storageMetadataLinkFact
	paths       []linuxStorageCanonicalPath
}

func sameStorageMetadataLink(a, b os.FileInfo) bool {
	return !nilStorageMetadata(a) && !nilStorageMetadata(b) &&
		a.Mode().Type() == os.ModeSymlink && b.Mode().Type() == os.ModeSymlink &&
		os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func sameStorageMetadataLinkFact(a, b storageMetadataLinkFact) bool {
	if a.path != b.path || a.absent != b.absent || a.target != b.target {
		return false
	}
	if a.absent {
		return a.info == nil && b.info == nil && a.target == ""
	}
	return sameStorageMetadataLink(a.info, b.info)
}

func observeStorageMetadataLink(ctx context.Context, root storageMetadataRoot, path string, optional bool) (storageMetadataLinkFact, error) {
	fail := func() (storageMetadataLinkFact, error) { return storageMetadataLinkFact{}, errStorageMetadataAcquire }
	if nilStorageMetadata(root) {
		return fail()
	}
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataLinkFact{}, err
	}
	before, err := root.Lstat(path)
	if err != nil {
		if !optional || !os.IsNotExist(err) {
			return fail()
		}
		if err := storageMetadataStop(ctx); err != nil {
			return storageMetadataLinkFact{}, err
		}
		_, err = root.Lstat(path)
		if !os.IsNotExist(err) {
			return fail()
		}
		if err := storageMetadataStop(ctx); err != nil {
			return storageMetadataLinkFact{}, err
		}
		return storageMetadataLinkFact{path: path, absent: true}, nil
	}
	if nilStorageMetadata(before) || before.Mode().Type() != os.ModeSymlink {
		return fail()
	}
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataLinkFact{}, err
	}
	target, err := root.Readlink(path)
	// Root.Readlink bounds parent traversal, not returned target text. Its SDK
	// buffer allocation precedes this result bound; this is no allocation cap.
	if err != nil || len(target) == 0 || len(target) > 4096 {
		return fail()
	}
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataLinkFact{}, err
	}
	after, err := root.Lstat(path)
	if err != nil || !sameStorageMetadataLink(before, after) {
		return fail()
	}
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataLinkFact{}, err
	}
	return storageMetadataLinkFact{path: path, target: target, info: before}, nil
}

func storageMetadataClassPath(name, target string) ([]string, error) {
	if !linuxStorageName(name) || len(target) > 4096 || !strings.HasPrefix(target, "../../devices/") {
		return nil, errStorageMetadataAcquire
	}
	canonical := "/" + strings.TrimPrefix(target, "../../")
	parts, err := linuxStorageCanonicalParts(canonical)
	if err != nil || len(parts) < 2 || parts[len(parts)-1] != name {
		return nil, errStorageMetadataAcquire
	}
	chain := make([]string, len(parts))
	for i := range parts {
		chain[i] = "/" + strings.Join(parts[:i+1], "/")
	}
	return chain, nil
}

func storageMetadataSubsystemPath(directory, target string) (string, string, error) {
	fail := func() (string, string, error) { return "", "", errStorageMetadataAcquire }
	base, err := linuxStorageCanonicalParts(directory)
	if err != nil || len(target) == 0 || len(target) > 4096 {
		return fail()
	}
	parts := strings.Split(target, "/")
	if len(parts) != len(base)+2 {
		return fail()
	}
	for i := range base {
		if parts[i] != ".." {
			return fail()
		}
	}
	kind, name := parts[len(base)], parts[len(base)+1]
	if kind != "bus" && kind != "class" || !linuxStorageName(name) || name == "none" {
		return fail()
	}
	if name == "block" && kind != "class" {
		return fail()
	}
	return "/" + kind + "/" + name, name, nil
}

// This reserves returned owners for a successful stable route. claim still runs
// after provider acquisition, and a rejected newly acquired owner must close.
// SDK-internal descriptors and syscall counts are not this registry's bound.
func storageMetadataAncestryBudget(nodes, edges, ancestors int) error {
	if nodes < 0 || nodes > 256 || edges < 0 || edges > 1024 || ancestors < 0 || ancestors > 1024 {
		return errStorageMetadataAcquire
	}
	if 15+33*nodes+2*edges+6*ancestors > storageMetadataMaximumOwners {
		return errStorageMetadataAcquire
	}
	return nil
}

func planStorageMetadataAncestry(ctx context.Context, root storageMetadataRoot, capture linuxStorageCapture) (storageMetadataAncestryPlan, error) {
	fail := func() (storageMetadataAncestryPlan, error) {
		return storageMetadataAncestryPlan{}, errStorageMetadataAcquire
	}
	if capture.registry == nil || capture.nodes == nil || len(capture.registry) > 256 || len(capture.registry) != len(capture.nodes) {
		return fail()
	}
	plan := storageMetadataAncestryPlan{chains: map[string][]string{}, paths: []string{}, links: map[string]storageMetadataLinkFact{}}
	nodes := map[string]bool{}
	edges := 0
	for _, node := range capture.nodes {
		if !linuxStorageName(node.name) || nodes[node.name] || node.holders == nil || node.slaves == nil {
			return fail()
		}
		nodes[node.name] = true
		for _, relation := range [][]string{node.holders, node.slaves} {
			if len(relation) > 1024-edges {
				return fail()
			}
			edges += len(relation)
		}
	}
	unique := map[string]bool{}
	for _, name := range capture.registry {
		if !nodes[name] {
			return fail()
		}
		if _, duplicate := plan.chains[name]; duplicate {
			return fail()
		}
		link, err := observeStorageMetadataLink(ctx, root, "class/block/"+name, false)
		if err != nil {
			return storageMetadataAncestryPlan{}, err
		}
		chain, err := storageMetadataClassPath(name, link.target)
		if err != nil {
			return fail()
		}
		plan.chains[name], plan.links[link.path] = chain, link
		for _, path := range chain {
			if unique[path] {
				continue
			}
			if len(unique) >= 1024 {
				return fail()
			}
			unique[path] = true
			plan.paths = append(plan.paths, path)
		}
	}
	if err := storageMetadataAncestryBudget(len(capture.registry), edges, len(unique)); err != nil {
		return fail()
	}
	sort.Strings(plan.paths)
	return plan, nil
}

func storageMetadataUSBType(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 4096 || raw[len(raw)-1] != '\n' {
		return "", errStorageMetadataAcquire
	}
	seen := map[string]bool{}
	typeName := ""
	for _, line := range strings.Split(string(raw[:len(raw)-1]), "\n") {
		index := strings.IndexByte(line, '=')
		if index <= 0 || index > 64 {
			return "", errStorageMetadataAcquire
		}
		key, value := line[:index], line[index+1:]
		for _, c := range key {
			if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return "", errStorageMetadataAcquire
			}
		}
		for _, c := range value {
			if c < 32 || c > 126 {
				return "", errStorageMetadataAcquire
			}
		}
		if seen[key] {
			return "", errStorageMetadataAcquire
		}
		seen[key] = true
		if key == "DEVTYPE" {
			typeName = value
		}
	}
	if typeName != "usb_device" && typeName != "usb_interface" {
		return "", errStorageMetadataAcquire
	}
	return typeName, nil
}

func (state *storageMetadataAncestry) addAnchor(ctx context.Context, root storageMetadataRoot, path string) error {
	if prior, found := state.anchors[path]; found {
		return checkStorageMetadataAnchor(ctx, root, prior)
	}
	anchor, err := acquireStorageMetadataAnchor(ctx, root, strings.TrimPrefix(path, "/"), state.owners)
	if err != nil {
		return err
	}
	for _, prior := range state.anchors {
		if os.SameFile(anchor.info, prior.info) {
			return errStorageMetadataAcquire
		}
	}
	state.anchors[path] = anchor
	return nil
}

func (state *storageMetadataAncestry) verify(ctx context.Context, root storageMetadataRoot) error {
	anchorNames := make([]string, 0, len(state.anchors))
	for path := range state.anchors {
		anchorNames = append(anchorNames, path)
	}
	sort.Strings(anchorNames)
	for _, path := range anchorNames {
		if err := checkStorageMetadataAnchor(ctx, root, state.anchors[path]); err != nil {
			return err
		}
	}
	linkNames := make([]string, 0, len(state.links))
	for path := range state.links {
		linkNames = append(linkNames, path)
	}
	sort.Strings(linkNames)
	for _, path := range linkNames {
		current, err := observeStorageMetadataLink(ctx, root, path, true)
		if err != nil {
			return err
		}
		if !sameStorageMetadataLinkFact(state.links[path], current) {
			return errStorageMetadataAcquire
		}
	}
	return storageMetadataStop(ctx)
}

func (state *storageMetadataAncestry) observe(ctx context.Context, root storageMetadataRoot, capture linuxStorageCapture, nodes map[string]storageMetadataAnchor, first bool) ([]linuxStorageCanonicalPath, error) {
	byPath := map[string]linuxStorageAncestor{}
	for _, path := range state.plan.paths {
		if err := checkStorageMetadataAnchor(ctx, root, state.anchors[path]); err != nil {
			return nil, err
		}
		linkPath := path[1:] + "/subsystem"
		link, err := observeStorageMetadataLink(ctx, root, linkPath, true)
		if err != nil {
			return nil, err
		}
		if !first && !sameStorageMetadataLinkFact(state.links[linkPath], link) {
			return nil, errStorageMetadataAcquire
		}
		ancestor := linuxStorageAncestor{path: path, subsystem: "none"}
		if !link.absent {
			target, subsystem, err := storageMetadataSubsystemPath(path, link.target)
			if err != nil {
				return nil, err
			}
			if first {
				if err := state.addAnchor(ctx, root, target); err != nil {
					return nil, err
				}
			} else {
				anchor, found := state.anchors[target]
				if !found {
					return nil, errStorageMetadataAcquire
				}
				if err := checkStorageMetadataAnchor(ctx, root, anchor); err != nil {
					return nil, err
				}
			}
			ancestor.subsystem = subsystem
		}
		if first {
			state.links[linkPath] = link
		}
		switch ancestor.subsystem {
		case "usb":
			raw, err := readStorageMetadata(ctx, root, path[1:]+"/uevent", 4096)
			if err != nil {
				return nil, err
			}
			ancestor.deviceType, err = storageMetadataUSBType(raw)
			if err != nil {
				return nil, err
			}
		case "scsi":
			missing, err := storageMetadataAbsent(ctx, root, path[1:]+"/type")
			if err != nil {
				return nil, err
			}
			if !missing {
				ancestor.scsiType, err = readStorageMetadata(ctx, root, path[1:]+"/type", 256)
				if err != nil {
					return nil, err
				}
				number, err := linuxStorageScalar(ancestor.scsiType, 32)
				if err != nil || number > 31 {
					return nil, errStorageMetadataAcquire
				}
			}
		}
		if err := checkStorageMetadataAnchor(ctx, root, state.anchors[path]); err != nil {
			return nil, err
		}
		byPath[path] = ancestor
	}
	byName := map[string]linuxStorageCapturedNode{}
	for _, node := range capture.nodes {
		byName[node.name] = node
	}
	paths := make([]linuxStorageCanonicalPath, 0, len(capture.registry))
	for _, name := range capture.registry {
		chain := state.plan.chains[name]
		if len(chain) < 2 {
			return nil, errStorageMetadataAcquire
		}
		canonical := state.anchors[chain[len(chain)-1]]
		held, found := nodes[name]
		if !found || !sameStorageMetadata(canonical.info, held.info, true) {
			return nil, errStorageMetadataAcquire
		}
		if err := checkStorageMetadataAnchor(ctx, root, held); err != nil {
			return nil, err
		}
		number, err := readStorageMetadata(ctx, root, chain[len(chain)-1][1:]+"/dev", 256)
		if err != nil {
			return nil, err
		}
		actual, err := linuxStorageNumber(number, true)
		expected, expectedErr := linuxStorageNumber(byName[name].number, true)
		if err != nil || expectedErr != nil || actual != expected {
			return nil, errStorageMetadataAcquire
		}
		row := linuxStorageCanonicalPath{name: name, number: slices.Clone(number), ancestors: make([]linuxStorageAncestor, 0, len(chain))}
		for _, path := range chain {
			ancestor, found := byPath[path]
			if !found {
				return nil, errStorageMetadataAcquire
			}
			ancestor.scsiType = slices.Clone(ancestor.scsiType)
			row.ancestors = append(row.ancestors, ancestor)
		}
		paths = append(paths, row)
	}
	return paths, storageMetadataStop(ctx)
}

func (state *storageMetadataAncestry) acquire(ctx context.Context, root storageMetadataRoot, capture linuxStorageCapture, nodes map[string]storageMetadataAnchor) error {
	if state == nil || state.owners == nil || state.initialized {
		return errStorageMetadataAcquire
	}
	plan, err := planStorageMetadataAncestry(ctx, root, capture)
	if err != nil {
		return err
	}
	state.plan, state.links = plan, plan.links
	state.anchors = map[string]storageMetadataAnchor{}
	for _, path := range plan.paths {
		if err := state.addAnchor(ctx, root, path); err != nil {
			return err
		}
	}
	state.paths, err = state.observe(ctx, root, capture, nodes, true)
	if err != nil {
		return err
	}
	if err := state.verify(ctx, root); err != nil {
		return err
	}
	state.initialized = true
	return nil
}

func (state *storageMetadataAncestry) finish(ctx context.Context, root storageMetadataRoot, capture linuxStorageCapture, nodes map[string]storageMetadataAnchor) error {
	if state == nil || !state.initialized {
		return nil
	}
	if err := state.verify(ctx, root); err != nil {
		return err
	}
	final, err := state.observe(ctx, root, capture, nodes, false)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(state.paths, final) {
		return errStorageMetadataAcquire
	}
	return state.verify(ctx, root)
}

// Classify returns descriptive copies only. It never supplies boot provenance or
// target authority. Its values remain provisional until the owning scope closes.
func (view *storageMetadataView) Classify(ctx context.Context) (linuxStorageMetadataInput, error) {
	if err := view.lockActive(); err != nil {
		return linuxStorageMetadataInput{}, err
	}
	defer view.mu.Unlock()
	fail := func(err error) (linuxStorageMetadataInput, error) {
		view.failure = err
		view.poisoned.Store(true)
		return linuxStorageMetadataInput{}, err
	}
	if err := storageMetadataStop(ctx); err != nil {
		return fail(err)
	}
	if view.ancestry == nil {
		return fail(errStorageMetadataAcquire)
	}
	if !view.ancestry.initialized {
		if err := view.ancestry.acquire(ctx, view.root, view.snapshot, view.anchors); err != nil {
			return fail(err)
		}
	}
	out, err := classifyLinuxStorageCapture(view.snapshot, view.ancestry.paths)
	if err != nil {
		return fail(err)
	}
	if err := storageMetadataStop(ctx); err != nil {
		return fail(err)
	}
	if view.poisoned.Load() {
		return fail(errStorageMetadataView)
	}
	return out, nil
}

// A connected internal metadata route, not an active media-sampling entry.
// Unknown boot remains nil, so the strict parser still cannot admit this input.
func captureClassifiedLinuxStorageMetadata(ctx context.Context, operations storageMetadataOperations) (linuxStorageMetadataInput, error) {
	var provisional linuxStorageMetadataInput
	_, err := withLinuxStorageMetadata(ctx, operations, func(view *storageMetadataView) error {
		var err error
		provisional, err = view.Classify(ctx)
		return err
	})
	if err != nil {
		return linuxStorageMetadataInput{}, err
	}
	return provisional, nil
}
