//
//  WatchAddResultModel.swift
//  CBRPlayer
//
//  Ответ 201 на POST /watchlist (services/api/internal/httpapi/watchlist.go, addToWatchlist):
//  итог первой проверки пользователя recorder'ом.
//

import Foundation

struct WatchAddResultModel: Equatable, Codable {
    /// Имя, под которым пользователь сохранён (из ссылки берётся последний сегмент пути).
    let user: String
    /// Итог проверки: `watching`, `blocked`, `not_found`, `disabled`, `pending`
    /// (recorder не успел за 30 с) или `recorder_offline` (Go `checkNames`).
    let check: String
    let roomStatus: String?
    let reason: String?

    /// recorder не запущен (нет heartbeat в базе): пользователь добавлен,
    /// проверка пройдёт при запуске recorder'а.
    static let checkRecorderOffline = "recorder_offline"

    var isRecorderOffline: Bool { check == Self.checkRecorderOffline }
}
