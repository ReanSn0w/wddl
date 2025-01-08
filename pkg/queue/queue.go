package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/boltdb/bolt"
	"github.com/go-pkgz/lgr"
)

var (
	queueBucket = []byte("queue")
)

func New(path string) (*Queue, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) (err error) {
		_, err = tx.CreateBucketIfNotExists(queueBucket)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	q := &Queue{
		db: db,
	}

	return q, nil
}

type Queue struct {
	db *bolt.DB
}

// Add - добавляет файл в очередь
func (q *Queue) Add(file engine.File) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(queueBucket)
		if err != nil {
			return err
		}

		key := []byte(fmt.Sprintf("%d", file.ID))

		buf := new(bytes.Buffer)
		err = json.NewEncoder(buf).Encode(file)
		if err != nil {
			return err
		}

		err = bucket.Put(key, buf.Bytes())
		if err != nil {
			return err
		}

		return nil
	})
}

// Exists - проверяет наличие файла в очереди
// в случае его отсутствия возвращает ошибку
func (q *Queue) Exists(id string) error {
	return q.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return engine.ErrNotFound
		}

		key := []byte(id)
		if bucket.Get(key) == nil {
			return engine.ErrNotFound
		}

		return nil
	})
}

// Len - возвращает количество файлов в очереди
// в случае их отсутствия возвращает (0, nil)
func (q *Queue) Len() (int, error) {
	var (
		count int
		err   error
	)

	err = q.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return engine.ErrNotFound
		}

		count = bucket.Stats().KeyN
		return nil
	})

	return count, err
}

// List - возвращает список файлов из очереди
// в случае случае их отсутсвия возвращает (nil, nil)
func (q *Queue) List(filter func(f engine.File) error) ([]engine.File, error) {
	var (
		result []engine.File
		err    error
	)

	err = q.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return engine.ErrNotFound
		}

		c := bucket.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var file engine.File
			err := json.NewDecoder(bytes.NewReader(v)).Decode(&file)
			if err != nil {
				return err
			}

			if filter != nil {
				if err := filter(file); err != nil {
					return err
				}
			}

			result = append(result, file)
		}

		return nil
	})

	return result, err
}

// Chan - возвращает канал с файлами из очереди
// в случае их присутствия в очереди в противном случае породит go рутину
// которая будет периодически опрашивать очередь на наличие новых файлов
func (q *Queue) Chan(ctx context.Context, log lgr.L, filter func(f engine.File) error) <-chan engine.File {
	ch := make(chan engine.File)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(time.Second * 3)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				items, err := q.List(filter)
				if err != nil {
					log.Logf("[ERROR] listing files: %v", err)
					continue
				}

				for _, item := range items {
					ch <- item
				}
			default:
				time.Sleep(time.Millisecond * 100)
			}
		}
	}()

	return ch
}

// Delete - удаляет файл из очереди
// в случае его присутствия в очереди
func (q *Queue) Delete(id string) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return nil
		}

		key := []byte(id)
		if bucket.Get(key) == nil {
			return nil
		}

		return bucket.Delete(key)
	})
}
