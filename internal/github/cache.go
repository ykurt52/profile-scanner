package github

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Cache keeps ETag-tagged responses on disk. It only holds public repo data.
type Cache struct {
	Dir string
}

type Entry struct {
	ETag string `json:"etag"`
	Body []byte `json:"body"`
}

func NewCache(dir string) *Cache {
	_ = os.MkdirAll(dir, 0o700)
	return &Cache{Dir: dir}
}

func (c *Cache) file(key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(c.Dir, hex.EncodeToString(h[:16])+".json")
}

func (c *Cache) Get(key string) *Entry {
	b, err := os.ReadFile(c.file(key))
	if err != nil {
		return nil
	}
	var e Entry
	if json.Unmarshal(b, &e) != nil {
		return nil
	}
	return &e
}

func (c *Cache) Put(key string, e *Entry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.file(key), b, 0o600)
}
