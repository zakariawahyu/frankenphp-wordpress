package cache


import (
	"os"
	"go.uber.org/zap"
	"strings"
	"errors"
	"time"
	"strconv"
)

type Store struct {
	loc string
	ttl int
	logger *zap.Logger
	memCache map[string]*MemCacheItem
}

type MemCacheItem struct {
	content map[int]*string
	value string
	timestamp int64
}

const (
	CACHE_DIR = "sidekick-cache"
)


func NewStore(loc string, ttl int, logger *zap.Logger) *Store {
	os.MkdirAll(loc+"/"+CACHE_DIR, os.ModePerm)
	memCache := make(map[string]*MemCacheItem)

	// // Load cache from disk
	files, err := os.ReadDir(loc+"/"+CACHE_DIR)
	if err == nil {
		for _, file := range files {
			if file.IsDir() {
				pageFiles, err := os.ReadDir(loc+"/"+CACHE_DIR+"/"+file.Name())
				if err != nil {
					continue
				}

				memCache[file.Name()] = &MemCacheItem{
					content: make(map[int]*string),
					value: "",
					timestamp: time.Now().Unix(),
				}

				for idx, pageFile := range pageFiles {
					if !pageFile.IsDir() {
						value, err := os.ReadFile(loc+"/"+CACHE_DIR+"/"+file.Name()+"/"+pageFile.Name())

						if err != nil {
							continue
						}
						newValue := string(value)
						memCache[file.Name()].content[idx] = &newValue
						memCache[file.Name()].value += newValue
					}
				}
			}
		}
	}

	return &Store{
		loc: loc,
		ttl: ttl,
		logger: logger,
		memCache: memCache,
	}
}


func (d *Store) Get(key string) ([]byte, error) {
	key = strings.ReplaceAll(key, "/", "+")
	d.logger.Debug("Getting key from cache", zap.String("key", key))

	if d.memCache[key] != nil {
		d.logger.Debug("Pulled key from memory", zap.String("key", key))

		if time.Now().Unix() - d.memCache[key].timestamp > int64(d.ttl) {
			d.logger.Debug("Cache expired", zap.String("key", key))
			go d.Purge(key)
			return nil, errors.New("Cache expired")
		}

		d.logger.Debug("Cache hit", zap.String("key", key))
		return []byte(d.memCache[key].value), nil
	}

	// load files in directory
	files, err := os.ReadDir(d.loc+"/"+CACHE_DIR+"/"+key)
	if err != nil {
		return nil, errors.New("Key not found in cache")
	}

	content := ""

	for _, file := range files {
		if !file.IsDir() {
			value, err := os.ReadFile(d.loc+"/"+CACHE_DIR+"/"+key+"/"+file.Name())
			if err != nil {
				return nil, errors.New("Key not found in cache")
			}

			content += string(value)
		}
	}

	d.logger.Debug("Cache hit", zap.String("key", key))
	d.logger.Debug("Pulled key from disk", zap.String("key", key))

	return []byte(content), nil
}

func (d *Store) Set(key string, idx int, value []byte) error {
	key = strings.ReplaceAll(key, "/", "+")

	if d.memCache[key] == nil {
		d.memCache[key] = &MemCacheItem{
			content: make(map[int]*string),
			value: "",
			timestamp: time.Now().Unix(),
		}
	}
	
	d.logger.Debug("-----------------------------------")
	d.logger.Debug("Setting key in cache", zap.String("key", key))
	d.logger.Debug("Index", zap.Int("index", idx))
	newValue := string(value)

	if idx == 0 {
		d.memCache[key].timestamp = time.Now().Unix()
	}

	d.memCache[key].value += newValue

	// create page directory 
	os.MkdirAll(d.loc+"/"+CACHE_DIR+"/"+key, os.ModePerm)
	err := os.WriteFile(d.loc+"/"+CACHE_DIR+"/"+key+"/"+strconv.Itoa(idx), value, os.ModePerm)
	
	if err != nil {
		d.logger.Error("Error writing to cache", zap.Error(err))
	}

	return nil
}

func (d *Store) Purge(key string) {
	key = strings.ReplaceAll(key, "/", "+")
	d.logger.Debug("Removing key from cache", zap.String("key", key))

	delete(d.memCache, "br::"+key)
	delete(d.memCache, "gzip::"+key)
	delete(d.memCache, "none::"+key)
	
	os.RemoveAll(d.loc+"/"+CACHE_DIR+"/br::"+key)
	os.RemoveAll(d.loc+"/"+CACHE_DIR+"/gzip::"+key)
	os.RemoveAll(d.loc+"/"+CACHE_DIR+"/none::"+key)
}

func (d *Store) Flush() error {
	d.memCache = make(map[string]*MemCacheItem)
	err := os.RemoveAll(d.loc + "/" + CACHE_DIR)

	if err == nil {
		os.MkdirAll(d.loc+"/"+CACHE_DIR, os.ModePerm)
	} else {
		d.logger.Error("Error flushing cache", zap.Error(err))
	}

	return err
}

func (d *Store) List() map[string][]string {
	list := make(map[string][]string)
	list["mem"] = make([]string, len(d.memCache))
	memIdx := 0

	for key, _ := range d.memCache {
		list["mem"][memIdx] = key
		memIdx++
	}

	files, err := os.ReadDir(d.loc+"/"+CACHE_DIR)
	list["disk"] = make([]string, 0)

	if err == nil {
		for _, file := range files {
			if !file.IsDir() {
				list["disk"] = append(list["disk"], file.Name())
			}
		}
	}

	return list
}