package storagesample

import (
	"context"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

// This private raw capture has no authority flags or exported wire encoding.
// Partition inherited geometry, classification, USB ancestry and original boot
// backing are deliberately absent until their separate producers are reviewed.
type linuxStorageCapturedNode struct {
	name                                                          string
	number, sectors, readOnly, partition, removable, logicalBlock []byte
	holders, slaves                                               []string
	partitionSlavesAbsent                                         bool
}
type linuxStorageCapture struct {
	registry         []string
	nodes            []linuxStorageCapturedNode
	mountInfo, swaps []byte
}
type storageMetadataAnchor struct {
	path   string
	handle storageMetadataPath
	info   os.FileInfo
}

func acquireStorageMetadataAnchor(ctx context.Context, root storageMetadataRoot, path string, owners *[]io.Closer) (storageMetadataAnchor, error) {
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataAnchor{}, err
	}
	before, err := root.Lstat(path)
	if err != nil || !storageMetadataType(before, true) {
		return storageMetadataAnchor{}, errStorageMetadataAcquire
	}
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataAnchor{}, err
	}
	handle, err := root.OpenPath(path)
	if !nilStorageMetadata(handle) {
		*owners = append(*owners, handle)
	}
	if err != nil || nilStorageMetadata(handle) {
		return storageMetadataAnchor{}, errStorageMetadataAcquire
	}
	if err := storageMetadataStop(ctx); err != nil {
		return storageMetadataAnchor{}, err
	}
	info, err := handle.Stat()
	if err != nil || !sameStorageMetadata(before, info, true) {
		return storageMetadataAnchor{}, errStorageMetadataAcquire
	}
	anchor := storageMetadataAnchor{path, handle, info}
	if err := checkStorageMetadataAnchor(ctx, root, anchor); err != nil {
		return storageMetadataAnchor{}, err
	}
	return anchor, nil
}

func checkStorageMetadataAnchor(ctx context.Context, root storageMetadataRoot, anchor storageMetadataAnchor) error {
	if err := storageMetadataStop(ctx); err != nil {
		return err
	}
	info, err := anchor.handle.Stat()
	if err != nil || !sameStorageMetadata(anchor.info, info, true) {
		return errStorageMetadataAcquire
	}
	if err := storageMetadataStop(ctx); err != nil {
		return err
	}
	named, err := root.Lstat(anchor.path)
	if err != nil || !sameStorageMetadata(anchor.info, named, true) {
		return errStorageMetadataAcquire
	}
	return storageMetadataStop(ctx)
}

func storageMetadataAlias(ctx context.Context, root storageMetadataRoot, path string, expected storageMetadataAnchor) (err error) {
	owners := []io.Closer{}
	defer func() {
		closeStorageMetadata(owners, &err)
		if err == nil {
			err = storageMetadataStop(ctx)
		}
	}()
	actual, err := acquireStorageMetadataAnchor(ctx, root, path, &owners)
	if err != nil {
		return err
	}
	if !sameStorageMetadata(actual.info, expected.info, true) {
		return errStorageMetadataAcquire
	}
	return checkStorageMetadataAnchor(ctx, root, expected)
}

func storageMetadataAbsent(ctx context.Context, root storageMetadataRoot, path string) (bool, error) {
	if err := storageMetadataStop(ctx); err != nil {
		return false, err
	}
	_, err := root.Lstat(path)
	if err == nil {
		return false, storageMetadataStop(ctx)
	}
	if !os.IsNotExist(err) {
		return false, errStorageMetadataAcquire
	}
	if err := storageMetadataStop(ctx); err != nil {
		return false, err
	}
	_, err = root.Lstat(path)
	if !os.IsNotExist(err) {
		return false, errStorageMetadataAcquire
	}
	return true, storageMetadataStop(ctx)
}

func captureStorageMetadataNode(ctx context.Context, root storageMetadataRoot, name string, anchors map[string]storageMetadataAnchor, edges *int) (linuxStorageCapturedNode, error) {
	fail := func() (linuxStorageCapturedNode, error) { return linuxStorageCapturedNode{}, errStorageMetadataAcquire }
	if strings.HasPrefix(name, "loop") {
		return linuxStorageCapturedNode{}, errLinuxStorageLoop
	}
	anchor := anchors[name]
	if err := checkStorageMetadataAnchor(ctx, root, anchor); err != nil {
		return linuxStorageCapturedNode{}, err
	}
	base := "class/block/" + name + "/"
	node := linuxStorageCapturedNode{name: name}
	var err error
	if node.number, err = readStorageMetadata(ctx, root, base+"dev", 256); err != nil {
		return linuxStorageCapturedNode{}, err
	}
	number, err := linuxStorageNumber(node.number, true)
	if err != nil {
		return fail()
	}
	if strings.HasPrefix(number, "7:") {
		return linuxStorageCapturedNode{}, errLinuxStorageLoop
	}
	if node.sectors, err = readStorageMetadata(ctx, root, base+"size", 256); err != nil {
		return linuxStorageCapturedNode{}, err
	}
	sectors, err := linuxStorageScalar(node.sectors, 64)
	if err != nil || sectors > ^uint64(0)/512 {
		return fail()
	}
	if node.readOnly, err = readStorageMetadata(ctx, root, base+"ro", 256); err != nil {
		return linuxStorageCapturedNode{}, err
	}
	if _, err := linuxStorageScalar(node.readOnly, 1); err != nil {
		return fail()
	}
	absent, err := storageMetadataAbsent(ctx, root, base+"partition")
	if err != nil {
		return linuxStorageCapturedNode{}, err
	}
	if !absent {
		if node.partition, err = readStorageMetadata(ctx, root, base+"partition", 256); err != nil {
			return linuxStorageCapturedNode{}, err
		}
		part, err := linuxStorageScalar(node.partition, 32)
		if err != nil || part == 0 {
			return fail()
		}
	} else {
		if node.removable, err = readStorageMetadata(ctx, root, base+"removable", 256); err != nil {
			return linuxStorageCapturedNode{}, err
		}
		if _, err := linuxStorageScalar(node.removable, 1); err != nil {
			return fail()
		}
		if node.logicalBlock, err = readStorageMetadata(ctx, root, base+"queue/logical_block_size", 256); err != nil {
			return linuxStorageCapturedNode{}, err
		}
		if _, err := storagetest.ParseGeometry(node.sectors, node.logicalBlock); err != nil {
			return fail()
		}
	}
	for _, relation := range []string{"holders", "slaves"} {
		var names []string
		if relation == "slaves" && node.partition != nil {
			missing, e := storageMetadataAbsent(ctx, root, base+relation)
			if e != nil {
				return linuxStorageCapturedNode{}, e
			}
			if missing {
				// Linux partitions have holders but no per-partition slave_dir.
				// Keep the absence explicit; a later bridge must interpret it.
				node.partitionSlavesAbsent = true
				names = []string{}
			}
		}
		if names == nil {
			names, err = readStorageMetadataNames(ctx, root, base+relation, 1024-*edges)
			if err != nil {
				return linuxStorageCapturedNode{}, err
			}
		}
		*edges += len(names)
		for _, target := range names {
			expected, ok := anchors[target]
			if !ok || target == name {
				return fail()
			}
			// Resolve only canonical sysfs paths, never raw symlink text or /dev.
			if err := storageMetadataAlias(ctx, root, base+relation+"/"+target+"/.", expected); err != nil {
				return linuxStorageCapturedNode{}, err
			}
		}
		if relation == "holders" {
			node.holders = names
		} else {
			node.slaves = names
		}
	}
	if err := checkStorageMetadataAnchor(ctx, root, anchor); err != nil {
		return linuxStorageCapturedNode{}, err
	}
	return node, nil
}

func storageMetadataPass(ctx context.Context, sys, proc storageMetadataRoot, anchors map[string]storageMetadataAnchor, owners *[]io.Closer) (linuxStorageCapture, map[string]storageMetadataAnchor, error) {
	fail := func(err error) (linuxStorageCapture, map[string]storageMetadataAnchor, error) {
		return linuxStorageCapture{}, nil, err
	}
	names, err := readStorageMetadataNames(ctx, sys, "class/block", 256)
	if err != nil {
		return fail(err)
	}
	if anchors == nil {
		anchors = make(map[string]storageMetadataAnchor, len(names))
		for _, name := range names {
			if strings.HasPrefix(name, "loop") {
				return fail(errLinuxStorageLoop)
			}
			anchor, e := acquireStorageMetadataAnchor(ctx, sys, "class/block/"+name+"/.", owners)
			if e != nil {
				return fail(e)
			}
			for _, prior := range anchors {
				if os.SameFile(anchor.info, prior.info) {
					return fail(errStorageMetadataAcquire)
				}
			}
			anchors[name] = anchor
		}
	} else {
		if len(names) != len(anchors) {
			return fail(errStorageMetadataAcquire)
		}
		for _, name := range names {
			if _, ok := anchors[name]; !ok {
				return fail(errStorageMetadataAcquire)
			}
		}
	}
	result := linuxStorageCapture{registry: names, nodes: []linuxStorageCapturedNode{}}
	numbers := make(map[string]bool, len(names))
	edges := 0
	for _, name := range names {
		node, e := captureStorageMetadataNode(ctx, sys, name, anchors, &edges)
		if e != nil {
			return fail(e)
		}
		number, e := linuxStorageNumber(node.number, true)
		if e != nil || numbers[number] {
			return fail(errStorageMetadataAcquire)
		}
		numbers[number] = true
		result.nodes = append(result.nodes, node)
	}
	if result.mountInfo, err = readStorageMetadata(ctx, proc, "self/mountinfo", 4<<20); err != nil {
		return fail(err)
	}
	mounts, err := safemode.ParseMountFacts(result.mountInfo)
	if err != nil {
		return fail(errStorageMetadataAcquire)
	}
	for number := range mounts.BlockDevices {
		if !numbers[number] {
			return fail(errStorageMetadataAcquire)
		}
	}
	if result.swaps, err = readStorageMetadata(ctx, proc, "swaps", 64<<10); err != nil {
		return fail(err)
	}
	swap, err := parseLinuxStorageSwap(result.swaps)
	if err != nil {
		return fail(errStorageMetadataAcquire)
	}
	if swap != "none" {
		return fail(errLinuxStorageSwap)
	}
	for _, name := range names {
		if err := checkStorageMetadataAnchor(ctx, sys, anchors[name]); err != nil {
			return fail(err)
		}
	}
	return result, anchors, nil
}

// This observes two equal bounded captures, not an atomic topology transaction.
// Owners here retain metadata objects only: no block device or loop lifetime is
// proved. All classification, boot and live admission gates remain separate.
func captureLinuxStorageMetadata(ctx context.Context, operations storageMetadataOperations) (result linuxStorageCapture, err error) {
	if nilStorageMetadata(ctx) || operations.openRoot == nil {
		return linuxStorageCapture{}, errStorageMetadataAcquire
	}
	operations = uniqueStorageMetadataOperations(operations)
	owners := []io.Closer{}
	defer func() {
		closeStorageMetadata(owners, &err)
		if err == nil {
			err = storageMetadataStop(ctx)
		}
		if err != nil {
			result = linuxStorageCapture{}
		}
	}()
	roots := []storageMetadataRoot{}
	for _, name := range []string{"sysfs", "proc"} {
		if err = storageMetadataStop(ctx); err != nil {
			return linuxStorageCapture{}, err
		}
		root, e := operations.openRoot(name)
		if !nilStorageMetadata(root) {
			owners = append(owners, root)
		}
		if e != nil || nilStorageMetadata(root) {
			return linuxStorageCapture{}, errStorageMetadataAcquire
		}
		roots = append(roots, root)
	}
	first, anchors, err := storageMetadataPass(ctx, roots[0], roots[1], nil, &owners)
	if err != nil {
		return linuxStorageCapture{}, err
	}
	second, _, err := storageMetadataPass(ctx, roots[0], roots[1], anchors, &owners)
	if err != nil {
		return linuxStorageCapture{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return linuxStorageCapture{}, errStorageMetadataAcquire
	}
	return first, nil
}
