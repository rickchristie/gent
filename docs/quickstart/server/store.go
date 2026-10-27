package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	bolt "go.etcd.io/bbolt"
)

var errMissing = errors.New("run not found")
var errStaleWriter = errors.New("writer has been replaced")

type Store struct {
	db     *bolt.DB
	codecs *gent.CodecRegistry
}

func openStore(path string, codecs *gent.CodecRegistry) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	return &Store{db: db, codecs: codecs}, nil
}

// A new owner fences earlier workers. The check and state write use the same transaction.
func (s *Store) claim(runID string) (*Writer, error) {
	w := &Writer{store: s, runID: runID}
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(runID))
		if err != nil {
			return err
		}
		w.fence, err = bucket.NextSequence()
		if err != nil {
			return err
		}
		return bucket.Put([]byte("owner"), binary.BigEndian.AppendUint64(nil, w.fence))
	})
	return w, err
}

func (s *Store) read(bucket *bolt.Bucket, runID string) (gent.RunView, error) {
	view := gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: runID}
	data := bucket.Get([]byte("view"))
	if data == nil {
		return view, nil
	}
	err := s.codecs.Unmarshal(data, &view)
	return view, err
}

func (s *Store) load(runID string) (gent.RunView, error) {
	var view gent.RunView
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(runID))
		if bucket == nil {
			return errMissing
		}
		var err error
		view, err = s.read(bucket, runID)
		return err
	})
	return view, err
}

type Writer struct {
	store *Store
	runID string
	fence uint64
}

// Save is the Gent persistence boundary. Returning nil means the transaction has committed.
func (w *Writer) Save(ctx context.Context, batch *gent.SaveBatch) error {
	if batch.RunId != w.runID {
		return gent.ErrWrongRun
	}
	return w.store.db.Update(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		bucket := tx.Bucket([]byte(w.runID))
		if bucket == nil || binary.BigEndian.Uint64(bucket.Get([]byte("owner"))) != w.fence {
			return errStaleWriter
		}
		view, err := w.store.read(bucket, w.runID)
		if err != nil {
			return err
		}
		// ApplyBatch checks the previous cursor, event order, digest, and duplicate batch IDs.
		next, err := gent.ApplyBatch(view, batch, w.store.codecs)
		if err != nil {
			return fmt.Errorf("apply saved batch: %w", err)
		}
		data, err := w.store.codecs.Marshal(next)
		if err != nil {
			return err
		}
		return bucket.Put([]byte("view"), data)
	})
}
