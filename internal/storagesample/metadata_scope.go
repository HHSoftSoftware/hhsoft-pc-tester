package storagesample

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

var errStorageMetadataView = errors.New("storage metadata view is unavailable")
var errStorageMetadataConsumer = errors.New("storage metadata consumer failed")

// A view keeps metadata observations inside their owning call window. It has
// neither Check nor Close: it must never stand in for sampleTargetOwner.
type storageMetadataView struct {
	mu       sync.Mutex
	poisoned atomic.Bool
	active   bool
	failure  error
	snapshot linuxStorageCapture
	root     storageMetadataRoot
	registry storageMetadataAnchor
	anchors  map[string]storageMetadataAnchor
	ancestry *storageMetadataAncestry
}

func cloneLinuxStorageCapture(input linuxStorageCapture) linuxStorageCapture {
	out := input
	out.registry = slices.Clone(input.registry)
	out.nodes = slices.Clone(input.nodes)
	for i, node := range input.nodes {
		node.number = slices.Clone(node.number)
		node.sectors = slices.Clone(node.sectors)
		node.readOnly = slices.Clone(node.readOnly)
		node.partition = slices.Clone(node.partition)
		node.removable = slices.Clone(node.removable)
		node.logicalBlock = slices.Clone(node.logicalBlock)
		node.holders = slices.Clone(node.holders)
		node.slaves = slices.Clone(node.slaves)
		out.nodes[i] = node
	}
	out.mountInfo = slices.Clone(input.mountInfo)
	out.swaps = slices.Clone(input.swaps)
	return out
}

func (view *storageMetadataView) lockActive() error {
	if view == nil {
		return errStorageMetadataView
	}
	if !view.mu.TryLock() {
		view.poisoned.Store(true)
		return errStorageMetadataView
	}
	if !view.active || view.poisoned.Load() {
		view.mu.Unlock()
		return errStorageMetadataView
	}
	return nil
}

func (view *storageMetadataView) Snapshot() (linuxStorageCapture, error) {
	if err := view.lockActive(); err != nil {
		return linuxStorageCapture{}, err
	}
	defer view.mu.Unlock()
	result := cloneLinuxStorageCapture(view.snapshot)
	if view.poisoned.Load() {
		return linuxStorageCapture{}, errStorageMetadataView
	}
	return result, nil
}

// Verify observes the retained registry/node objects. It cannot prove an
// immutable registry, media identity, target ownership or continuous backing.
func (view *storageMetadataView) Verify(ctx context.Context) error {
	if err := view.lockActive(); err != nil {
		return err
	}
	defer view.mu.Unlock()
	err := checkStorageMetadataAnchor(ctx, view.root, view.registry)
	if err == nil {
		for _, name := range view.snapshot.registry {
			if err = checkStorageMetadataAnchor(ctx, view.root, view.anchors[name]); err != nil {
				break
			}
		}
	}
	if err == nil && view.ancestry != nil && view.ancestry.initialized {
		err = view.ancestry.verify(ctx, view.root)
	}
	if err == nil && view.poisoned.Load() {
		err = errStorageMetadataView
	}
	if err != nil {
		view.failure = err
		view.poisoned.Store(true)
	}
	return err
}

// Consumer-owned work must finish before it returns. Expiration waits for any
// already entered view operation; kernel calls are not hard-preemptible here.
func (view *storageMetadataView) expire() error {
	view.mu.Lock()
	defer view.mu.Unlock()
	view.active = false
	if view.failure == nil && view.poisoned.Load() {
		view.failure = errStorageMetadataView
	}
	view.snapshot = linuxStorageCapture{}
	view.root = nil
	view.registry = storageMetadataAnchor{}
	view.anchors = nil
	view.ancestry = nil
	return view.failure
}

// Keep raw observations provisional until the complete owning scope has closed.
// This wraps one initial and one final capture around the consumer, retaining
// roots and an explicit registry/node owner set throughout. It does not promote
// discovery flags, open media, or accept any target outcomes.
func withLinuxStorageMetadata(ctx context.Context, operations storageMetadataOperations, consume func(*storageMetadataView) error) (result linuxStorageCapture, err error) {
	if nilStorageMetadata(ctx) || operations.openRoot == nil || consume == nil {
		return linuxStorageCapture{}, errStorageMetadataAcquire
	}
	operations = uniqueStorageMetadataOperations(operations)
	owners := []io.Closer{}
	ancestry := &storageMetadataAncestry{owners: &owners}
	var view *storageMetadataView
	defer func() {
		if view != nil {
			err = errors.Join(err, view.expire())
		}
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
	registry, err := acquireStorageMetadataAnchor(ctx, roots[0], "class/block", &owners)
	if err != nil {
		return linuxStorageCapture{}, err
	}
	first, anchors, err := storageMetadataPass(ctx, roots[0], roots[1], nil, &owners)
	if err != nil {
		return linuxStorageCapture{}, err
	}
	if err = checkStorageMetadataAnchor(ctx, roots[0], registry); err != nil {
		return linuxStorageCapture{}, err
	}
	view = &storageMetadataView{active: true, snapshot: first, root: roots[0], registry: registry, anchors: anchors, ancestry: ancestry}
	if consume(view) != nil {
		return linuxStorageCapture{}, errStorageMetadataConsumer
	}
	if err = view.expire(); err != nil {
		return linuxStorageCapture{}, err
	}
	view = nil
	if err = storageMetadataStop(ctx); err != nil {
		return linuxStorageCapture{}, err
	}
	if err = checkStorageMetadataAnchor(ctx, roots[0], registry); err != nil {
		return linuxStorageCapture{}, err
	}
	second, _, err := storageMetadataPass(ctx, roots[0], roots[1], anchors, &owners)
	if err != nil {
		return linuxStorageCapture{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return linuxStorageCapture{}, errStorageMetadataAcquire
	}
	if err = ancestry.finish(ctx, roots[0], second, anchors); err != nil {
		return linuxStorageCapture{}, err
	}
	if err = checkStorageMetadataAnchor(ctx, roots[0], registry); err != nil {
		return linuxStorageCapture{}, err
	}
	return first, nil
}
