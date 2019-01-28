package hasher

import (
	"sort"

	"github.com/Nr90/imgsim"
	"github.com/rivo/duplo"
)

type Store interface {
	Add(key, hash interface{})
	Delete(key, hash interface{})
	Query(hash interface{}) interface{}
}

type DuploStore struct {
	store       *duplo.Store
	sensitivity int
}

func NewDuploStore(sensitivity int) *DuploStore {
	return &DuploStore{
		store:       duplo.New(),
		sensitivity: sensitivity,
	}
}

func (ds *DuploStore) Add(val, hash interface{}) {
	ds.store.Add(val, hash.(duplo.Hash))
}

func (ds *DuploStore) Delete(val, hash interface{}) {
	ds.store.Delete(val)
}

func (ds *DuploStore) Query(hash interface{}) interface{} {
	matches := ds.store.Query(hash.(duplo.Hash))
	if len(matches) > 0 {
		sort.Sort(matches)
		match := matches[0]
		if int(match.Score) <= ds.sensitivity {
			return match.ID
		}
	}
	return nil

}

type ImgsimStore struct {
	store map[imgsim.Hash]interface{}
}

func NewImgsimStore() *ImgsimStore {
	return &ImgsimStore{store: make(map[imgsim.Hash]interface{})}
}

func (is *ImgsimStore) Add(val, hash interface{}) {
	is.store[hash.(imgsim.Hash)] = val
}

func (is *ImgsimStore) Delete(val, hash interface{}) {
	delete(is.store, hash.(imgsim.Hash))
}

func (is *ImgsimStore) Query(hash interface{}) interface{} {
	return is.store[hash.(imgsim.Hash)]
}
