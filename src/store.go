package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

const bucketAPIKeys = "apikeys"

type APIKey struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Scopes      []string  `json:"scopes"`
	CreatedAt   time.Time `json:"created_at"`
}

type Store struct {
	db *bolt.DB
}

func openStore(path string) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketAPIKeys))
		return err
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("init db: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func (s *Store) GetAPIKey(key string) (*APIKey, error) {
	hashed := hashKey(key)
	var record APIKey
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketAPIKeys))
		data := b.Get([]byte(hashed))
		if data == nil {
			return fmt.Errorf("key not found")
		}
		return json.Unmarshal(data, &record)
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) PutAPIKey(key string, record *APIKey) error {
	hashed := hashKey(key)
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketAPIKeys))
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return b.Put([]byte(hashed), data)
	})
}

func (s *Store) DeleteAPIKey(key string) error {
	hashed := hashKey(key)
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketAPIKeys))
		return b.Delete([]byte(hashed))
	})
}

func (s *Store) ListAPIKeys() ([]APIKey, error) {
	var keys []APIKey
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketAPIKeys))
		return b.ForEach(func(_, v []byte) error {
			var record APIKey
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			keys = append(keys, record)
			return nil
		})
	})
	return keys, err
}
