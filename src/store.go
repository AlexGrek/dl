package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	bucketAPIKeys = "apikeys"
	bucketCache   = "cache"
	maxCacheTTL   = 6 * time.Hour
)

type APIKey struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Scopes      []string   `json:"scopes"`
	RootDir     string     `json:"root_dir,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLogin   *time.Time `json:"last_login,omitempty"`
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
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketAPIKeys)); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists([]byte(bucketCache))
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

func (s *Store) TouchLastLogin(key string) {
	hashed := hashKey(key)
	now := time.Now()
	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketAPIKeys))
		data := b.Get([]byte(hashed))
		if data == nil {
			return nil
		}
		var record APIKey
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		record.LastLogin = &now
		updated, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return b.Put([]byte(hashed), updated)
	})
}

func (s *Store) DeleteAPIKeyByID(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketAPIKeys))
		var found []byte
		if err := b.ForEach(func(k, v []byte) error {
			var record APIKey
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			if record.ID == id {
				found = k
			}
			return nil
		}); err != nil {
			return err
		}
		if found == nil {
			return fmt.Errorf("key not found")
		}
		return b.Delete(found)
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

// ── Cache (TTL-based key/value, used for product metadata) ──

type cacheEntry struct {
	Data      json.RawMessage `json:"d"`
	ExpiresAt time.Time       `json:"e"`
}

// GetCache returns the cached value for key, or nil if missing/expired.
func (s *Store) GetCache(key string) ([]byte, bool) {
	var result []byte
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketCache))
		if b == nil {
			return nil
		}
		raw := b.Get([]byte(key))
		if raw == nil {
			return nil
		}
		var entry cacheEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil
		}
		if time.Now().After(entry.ExpiresAt) {
			return nil
		}
		result = entry.Data
		return nil
	})
	return result, result != nil
}

// ClearCache deletes all cached entries.
func (s *Store) ClearCache() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte(bucketCache)); err != nil && err != bolt.ErrBucketNotFound {
			return err
		}
		_, err := tx.CreateBucket([]byte(bucketCache))
		return err
	})
}

// DeleteCache removes a single cached entry by key.
func (s *Store) DeleteCache(key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketCache))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// PutCache stores data under key with the given TTL (capped at maxCacheTTL).
func (s *Store) PutCache(key string, data []byte, ttl time.Duration) error {
	if ttl > maxCacheTTL {
		ttl = maxCacheTTL
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketCache))
		entry := cacheEntry{
			Data:      data,
			ExpiresAt: time.Now().Add(ttl),
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), raw)
	})
}
