// Package library — медиатека плеера: каждая папка с playlist.m3u8 в media/ — одно видео.
package library

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
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

// Playlist возвращает плейлист видео или nil, если playlist.m3u8 нет.
func (m *MediaInfo) Playlist() *Playlist {
	playlistPath := filepath.Join(m.EntryPath, "playlist.m3u8")
	if _, err := os.Stat(playlistPath); err == nil {
		return &Playlist{Path: playlistPath}
	}
	return nil
}
