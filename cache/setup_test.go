package cache

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/dgraph-io/badger/v3"
	"github.com/gomodule/redigo/redis"
)

var testRedisCache RedisCache
var testBadgerCache BadgerCache

func TestMain(m *testing.M) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}

	defer s.Close()

	pool := redis.Pool{
		MaxActive:   1000,
		MaxIdle:     50,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", s.Addr())
		},
	}

	testRedisCache.Conn = &pool
	testRedisCache.Prefix = "test_tjo:"

	defer testRedisCache.Conn.Close()

	_ = os.RemoveAll("./testdata/tmp/badger")

	if _, err := os.Stat("./testdata/tmp"); os.IsNotExist(err) {
		err := os.MkdirAll("./testdata/tmp", 0755)
		if err != nil {
			log.Fatal(err)
		}
	}

	// create a badger database
	db, _ := badger.Open(badger.DefaultOptions("./testdata/tmp/badger"))
	testBadgerCache.Conn = db

	os.Exit(m.Run())
}
