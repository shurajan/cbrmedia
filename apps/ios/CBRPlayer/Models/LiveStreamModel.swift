//
//  LiveStreamModel.swift
//  CBRPlayer
//
//  Ответ GET /watchlist/:user/live (services/api/internal/httpapi/live.go, LiveStream).
//

import Foundation

struct LiveStreamModel: Equatable, Codable {
    let user: String
    let roomStatus: String
    /// Ссылка на CDN с одноразовым токеном; открывается напрямую, без токена api.
    let hlsURL: String
}
