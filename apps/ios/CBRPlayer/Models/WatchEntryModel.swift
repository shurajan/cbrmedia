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
        case waiting
        case disabled(reason: String)
    }

    var status: Status {
        if !enabled { return .disabled(reason: disabledReason ?? "") }
        if recording { return .recording(roomStatus: roomStatus ?? "") }
        if (updatedAt ?? "").isEmpty { return .waiting }
        return .watching(roomStatus: roomStatus ?? "")
    }

    // ISO8601DateFormatter is documented as thread-safe
    private nonisolated(unsafe) static let iso8601Formatter = ISO8601DateFormatter()

    var addedDate: Date? {
        guard let addedAt else { return nil }
        return Self.iso8601Formatter.date(from: addedAt)
    }
}
