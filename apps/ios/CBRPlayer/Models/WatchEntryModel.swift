//
//  WatchEntryModel.swift
//  CBRPlayer
//
//  Элемент ответа GET /watchlist (services/api/internal/httpapi/watchlist.go, WatchEntry).
//

import Foundation

struct WatchEntryModel: Identifiable, Equatable, Codable {
    let user: String
    let addedAt: String?
    let addedBy: String?
    let enabled: Bool
    let disabledReason: String?
    let roomStatus: String?
    let recording: Bool
    /// Пусто, если recorder за пользователем сейчас не следит.
    let updatedAt: String?

    var id: String { user }

    /// Что сейчас происходит с пользователем.
    enum Status: Equatable {
        case recording(roomStatus: String)
        case watching(roomStatus: String)
        /// Status API отказывает в доступе (401/403) — recorder проверяет раз в сутки.
        case blocked
        /// Status API не находит пользователя (404/410) — проверка раз в сутки, после 3 раз подряд удаление.
        case notFound
        case waiting
        case disabled(reason: String)
    }

    /// Значения roomStatus, которые ставит recorder при ошибках status API
    /// (Go `store.RoomBlocked`, `store.RoomNotFound`).
    static let roomBlocked = "blocked"
    static let roomNotFound = "not found"
    /// Комната открыта — стрим можно смотреть (Go `roomPublic` в live.go).
    static let roomPublic = "public"

    /// Можно присоединиться к живому стриму: показывается кнопка play.
    var isLive: Bool { enabled && roomStatus == Self.roomPublic }

    var status: Status {
        if !enabled { return .disabled(reason: disabledReason ?? "") }
        if recording { return .recording(roomStatus: roomStatus ?? "") }
        if (updatedAt ?? "").isEmpty { return .waiting }
        switch roomStatus {
        case Self.roomBlocked: return .blocked
        case Self.roomNotFound: return .notFound
        default: return .watching(roomStatus: roomStatus ?? "")
        }
    }

    // ISO8601DateFormatter is documented as thread-safe
    private nonisolated(unsafe) static let iso8601Formatter = ISO8601DateFormatter()

    var addedDate: Date? {
        guard let addedAt else { return nil }
        return Self.iso8601Formatter.date(from: addedAt)
    }
}
