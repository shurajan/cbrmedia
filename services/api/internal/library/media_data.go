// Package library — медиатека плеера: каждая папка с playlist.m3u8 в media/ — одно видео,
// его метаданные — meta.json от converter (core/media).
package library

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"cbr/core/media"
)

// MediaInfo — одно видео медиатеки: папка baseDir/folder с playlist.m3u8.
type MediaInfo struct {
	BaseDir   string
	Folder    string
	EntryPath string
}

// NewMediaInfo описывает видео в папке baseDir/folder.
func NewMediaInfo(baseDir, folder string) *MediaInfo {
	return &MediaInfo{
		BaseDir:   baseDir,
		Folder:    folder,
		EntryPath: filepath.Join(baseDir, folder),
	}
}

// ID — стабильный идентификатор видео, вычисляемый из пути к папке.
func (m *MediaInfo) ID() string {
	return simpleID(m.EntryPath)
}

// CreatedAt — время изменения папки видео в RFC 3339 (UTC) или пустая строка.
func (m *MediaInfo) CreatedAt() string {
	info, err := os.Stat(m.EntryPath)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

// StreamURL — путь к плейлисту видео в HTTP API; имя папки экранировано.
func (m *MediaInfo) StreamURL() string {
	return fmt.Sprintf("/videos/%s/playlist.m3u8", url.PathEscape(m.Folder))
}

// Meta — метаданные видео из meta.json, который converter пишет при публикации
// (и дописывает старым видео при очередном проходе). false — в папке нет
// playlist.m3u8, это не видео. Нет или не читается meta.json — нули: видео в
// списке, converter заполнит файл в ближайшие минуты.
func (m *MediaInfo) Meta() (media.Meta, bool) {
	if _, err := os.Stat(filepath.Join(m.EntryPath, media.PlaylistFile)); err != nil {
		return media.Meta{}, false
	}
	meta, err := media.Read(m.EntryPath)
	if err != nil {
		return media.Meta{}, true
	}
	return meta, true
}
