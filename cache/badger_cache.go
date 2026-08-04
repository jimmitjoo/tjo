package cache

import (
	"path"
	"time"

	"github.com/dgraph-io/badger/v3"
)

type BadgerCache struct {
	Conn   *badger.DB
	Prefix string
}

func (b *BadgerCache) Has(str string) (bool, error) {
	key := b.Prefix + str
	_, err := b.Get(key)
	if err != nil {
		return false, nil
	}
	return err == nil, err
}

func (b *BadgerCache) Get(str string) (interface{}, error) {
	key := b.Prefix + str
	var fromCache []byte
	err := b.Conn.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			fromCache = append([]byte{}, val...)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	decoded, err := decode(string(fromCache))
	if err != nil {
		return nil, err
	}

	return decoded[str], nil
}

func (b *BadgerCache) Set(str string, value interface{}, ttl ...int) error {
	key := b.Prefix + str
	encoded, err := encode(Entry{str: value})
	if err != nil {
		return err
	}

	// Handle expiry
	if len(ttl) > 0 {
		b.Conn.Update(func(txn *badger.Txn) error {
			e := badger.NewEntry([]byte(key), encoded).WithTTL(time.Second * time.Duration(ttl[0]))
			err = txn.SetEntry(e)
			return err
		})
	} else {
		b.Conn.Update(func(txn *badger.Txn) error {
			e := badger.NewEntry([]byte(key), encoded)
			err = txn.SetEntry(e)
			return err
		})
	}

	return nil
}

func (b *BadgerCache) Forget(str string) error {
	key := b.Prefix + str
	return b.Conn.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

func (b *BadgerCache) EmptyByMatch(str string) error {
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false

	keysToDelete := make([][]byte, 0)

	err := b.Conn.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			// KeyCopy, not Key: badger reuses the buffer behind Key() on the
			// next iteration, so the accumulated slice would hold aliases to
			// overwritten memory by the time it is used.
			k := item.KeyCopy(nil)
			match, err := path.Match(str, string(k))
			if err != nil {
				return err
			}
			if match {
				keysToDelete = append(keysToDelete, k)
			}
		}
		return nil
	})

	err = b.Conn.Update(func(txn *badger.Txn) error {
		for _, k := range keysToDelete {
			err := txn.Delete(k)
			if err != nil {
				return err
			}
		}
		return nil
	})

	return err
}

func (b *BadgerCache) Flush() error {
	return b.EmptyByMatch("*")
}
