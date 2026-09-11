package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type memoryTableDriver struct {
	mu       sync.Mutex
	entities map[string]tableEntity
	nextETag int
}

func newMemoryTableDriver() *memoryTableDriver {
	return &memoryTableDriver{entities: make(map[string]tableEntity)}
}

func entityKey(value []byte) (string, string, error) {
	var header entityHeader
	if err := json.Unmarshal(value, &header); err != nil {
		return "", "", err
	}
	return header.PartitionKey, header.RowKey, nil
}

func (driver *memoryTableDriver) next() string {
	driver.nextETag++
	return fmt.Sprintf("etag-%d", driver.nextETag)
}

func (driver *memoryTableDriver) Get(ctx context.Context, partition, row string) (tableEntity, error) {
	if err := ctx.Err(); err != nil {
		return tableEntity{}, err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	entity, ok := driver.entities[partition+"\x00"+row]
	if !ok {
		return tableEntity{}, ErrNotFound
	}
	entity.Value = append([]byte(nil), entity.Value...)
	return entity, nil
}

func (driver *memoryTableDriver) Add(ctx context.Context, value []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	partition, row, err := entityKey(value)
	if err != nil {
		return "", err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	key := partition + "\x00" + row
	if _, ok := driver.entities[key]; ok {
		return "", ErrConflict
	}
	etag := driver.next()
	driver.entities[key] = tableEntity{Value: append([]byte(nil), value...), ETag: etag}
	return etag, nil
}

func (driver *memoryTableDriver) Update(ctx context.Context, value []byte, etag string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	partition, row, err := entityKey(value)
	if err != nil {
		return "", err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	key := partition + "\x00" + row
	stored, ok := driver.entities[key]
	if !ok {
		return "", ErrNotFound
	}
	if stored.ETag != etag {
		return "", ErrPrecondition
	}
	next := driver.next()
	driver.entities[key] = tableEntity{Value: append([]byte(nil), value...), ETag: next}
	return next, nil
}

func (driver *memoryTableDriver) Delete(ctx context.Context, partition, row, etag string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	key := partition + "\x00" + row
	stored, ok := driver.entities[key]
	if !ok {
		return ErrNotFound
	}
	if stored.ETag != etag {
		return ErrPrecondition
	}
	delete(driver.entities, key)
	return nil
}

func (driver *memoryTableDriver) List(ctx context.Context, filter string, maximum int32) ([]tableEntity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	result := make([]tableEntity, 0)
	for key, entity := range driver.entities {
		if strings.HasPrefix(key, partitionFromFilter(filter)+"\x00") {
			if id := propertyFromFilter(filter, "id"); id != "" {
				var value map[string]any
				if err := json.Unmarshal(entity.Value, &value); err != nil || value["id"] != id {
					continue
				}
			}
			copy := entity
			copy.Value = append([]byte(nil), entity.Value...)
			result = append(result, copy)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		pi, ri, _ := entityKey(result[i].Value)
		pj, rj, _ := entityKey(result[j].Value)
		return pi+ri < pj+rj
	})
	if len(result) > int(maximum) {
		result = result[:maximum]
	}
	return result, nil
}

func propertyFromFilter(filter, property string) string {
	prefix := property + " eq '"
	start := strings.Index(filter, prefix)
	if start < 0 {
		return ""
	}
	rest := filter[start+len(prefix):]
	end := strings.IndexByte(rest, '\'')
	if end < 0 {
		return ""
	}
	return strings.ReplaceAll(rest[:end], "''", "'")
}

func partitionFromFilter(filter string) string {
	const prefix = "PartitionKey eq '"
	start := strings.Index(filter, prefix)
	if start < 0 {
		return ""
	}
	rest := filter[start+len(prefix):]
	end := strings.IndexByte(rest, '\'')
	if end < 0 {
		return ""
	}
	return strings.ReplaceAll(rest[:end], "''", "'")
}

func (driver *memoryTableDriver) Transaction(ctx context.Context, actions []tableAction) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	clone := make(map[string]tableEntity, len(driver.entities))
	for key, value := range driver.entities {
		clone[key] = value
	}
	for _, action := range actions {
		partition, row, err := entityKey(action.Entity)
		if err != nil {
			return err
		}
		key := partition + "\x00" + row
		stored, ok := clone[key]
		switch action.Kind {
		case tableAdd:
			if ok {
				return ErrConflict
			}
			clone[key] = tableEntity{Value: append([]byte(nil), action.Entity...), ETag: driver.next()}
		case tableReplace:
			if !ok {
				return ErrNotFound
			}
			if stored.ETag != action.ETag {
				return ErrPrecondition
			}
			clone[key] = tableEntity{Value: append([]byte(nil), action.Entity...), ETag: driver.next()}
		case tableDelete:
			if !ok {
				return ErrNotFound
			}
			if stored.ETag != action.ETag {
				return ErrPrecondition
			}
			delete(clone, key)
		}
	}
	driver.entities = clone
	return nil
}
func (driver *memoryTableDriver) Probe(context.Context) error { return nil }

type memoryBlobDriver struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newMemoryBlobDriver() *memoryBlobDriver {
	return &memoryBlobDriver{values: make(map[string][]byte)}
}
func (d *memoryBlobDriver) PutImmutable(ctx context.Context, name string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.values[name]; ok {
		return ErrPrecondition
	}
	d.values[name] = append([]byte(nil), value...)
	return nil
}
func (d *memoryBlobDriver) Get(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.values[name]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}
func (d *memoryBlobDriver) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.values[name]; !ok {
		return ErrNotFound
	}
	delete(d.values, name)
	return nil
}
func (d *memoryBlobDriver) Probe(context.Context) error { return nil }
