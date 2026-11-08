//
//  HomeViewModel.swift
//  CBRPlayer
//
//  Сводка для плиток главной: GET /videos и GET /watchlist параллельно.
//  Ошибки не показываются — их покажет дочерний экран; сводка просто остаётся
//  прежней (или пустой при первой загрузке).
//

import SwiftUI

@MainActor
final class HomeViewModel: ObservableObject {
    @Published private(set) var videos: [VideoItemModel]?
    @Published private(set) var entries: [WatchEntryModel]?

    /// «48 videos · 112 GB»; nil, пока список видео не загружен.
    var videosSummary: String? {
        guard let videos else { return nil }
        let sizeMB = videos.reduce(0) { $0 + ($1.sizeMB ?? 0) }
        return "\(Self.count(videos.count, "video", "videos")) · \(VideoItemModel.formatSize(mb: sizeMB))"
    }

    /// «5 users»; nil, пока список слежения не загружен.
    var watchlistSummary: String? {
        guard let entries else { return nil }
        return Self.count(entries.count, "user", "users")
    }

    /// «1 recording» — сколько пользователей записывается сейчас; nil, если никого.
    var recordingSummary: String? {
        guard let entries else { return nil }
        let recording = entries.filter { $0.recording && $0.enabled }.count
        return recording > 0 ? Self.count(recording, "recording", "recordings") : nil
    }

    func load(host: String, token: String) async {
        async let videosResult = VideoService.shared.fetchFiles(host: host, token: token)
        async let entriesResult = WatchlistService.shared.fetch(host: host, token: token)
        if case .success(let fetched) = await videosResult { videos = fetched }
        if case .success(let fetched) = await entriesResult { entries = fetched }
    }

    private static func count(_ n: Int, _ one: String, _ many: String) -> String {
        "\(n) \(n == 1 ? one : many)"
    }
}
