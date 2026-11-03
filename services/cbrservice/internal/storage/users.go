// Package storage хранит список отслеживаемых пользователей в bbolt.
package storage

import (
	"errors"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

var usersBucket = []byte("users")

// Ошибки хранилища. Тексты показываются пользователю в Telegram.
var (
	ErrUserExists   = errors.New("user already exists")
	ErrUserNotFound = errors.New("user not found")
)

// Users — список отслеживаемых пользователей.
type Users struct {
	db *bbolt.DB
}

// OpenUsers открывает (или создаёт) базу по пути path.
// Если база заблокирована другим процессом, через секунду возвращается ошибка.
func OpenUsers(path string) (*Users, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open users db %s: %w", path, err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(usersBucket)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init users bucket: %w", err)
	}

	return &Users{db: db}, nil
}

// Add добавляет пользователя. Возвращает ErrUserExists, если он уже есть.
func (u *Users) Add(user string) error {
	return u.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		if b.Get([]byte(user)) != nil {
			return ErrUserExists
		}
		return b.Put([]byte(user), []byte{})
	})
}

// Remove удаляет пользователя. Возвращает ErrUserNotFound, если его нет.
func (u *Users) Remove(user string) error {
	return u.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		if b.Get([]byte(user)) == nil {
			return ErrUserNotFound
		}
		return b.Delete([]byte(user))
	})
}

// List возвращает всех пользователей в порядке ключей.
func (u *Users) List() ([]string, error) {
	var users []string
	err := u.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(usersBucket).ForEach(func(k, _ []byte) error {
			users = append(users, string(k))
			return nil
		})
	})
	return users, err
}

// Close закрывает базу.
func (u *Users) Close() error {
	return u.db.Close()
}
