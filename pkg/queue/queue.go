package queue

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/boltdb/bolt"
)

var (
	bucketName = []byte("queue")
)

func New(path string) (*Queue, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &Queue{q: db}, nil
}

type Queue struct {
	q *bolt.DB
}

func (q *Queue) Client() *bolt.DB {
	return q.q
}

func (q *Queue) List(ctx context.Context, limit int, filter func(engine.File) bool) ([]engine.File, error) {
	var files []engine.File

	if err := q.q.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

		c := b.Cursor()

		for k, v := c.First(); k != nil && len(files) < limit; k, v = c.Next() {
			var f engine.File
			if err := json.Unmarshal(v, &f); err != nil {
				return err
			}

			if filter(f) {
				files = append(files, f)
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return files, nil
}

func (q *Queue) Upsert(ctx context.Context, f engine.File) error {
	if err := q.q.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		buffer := new(bytes.Buffer)

		err := json.NewEncoder(buffer).Encode(f)
		if err != nil {
			return err
		}

		err = b.Put([]byte(f.ID), buffer.Bytes())
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (q *Queue) Get(ctx context.Context, id string) (*engine.File, error) {
	var f engine.File

	if err := q.q.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

		v := b.Get([]byte(id))
		if v == nil {
			return engine.ErrNotFound
		}

		if err := json.Unmarshal(v, &f); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &f, nil
}

func (q *Queue) MarkPartComplete(ctx context.Context, id string, part string) error {
	if err := q.q.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

		v := b.Get([]byte(id))
		if v == nil {
			return engine.ErrNotFound
		}

		var f engine.File
		if err := json.Unmarshal(v, &f); err != nil {
			return err
		}

		for i, p := range f.Parts {
			if p.ID == part {
				f.Parts[i].Complete = true
				f.CompleteParts++
			}
		}

		buffer := new(bytes.Buffer)

		err := json.NewEncoder(buffer).Encode(f)
		if err != nil {
			return err
		}

		err = b.Put([]byte(f.ID), buffer.Bytes())
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (q *Queue) Delete(ctx context.Context, id string) error {
	if err := q.q.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

		if err := b.Delete([]byte(id)); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}
