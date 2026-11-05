//
//  MediaListViewModel.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 22.04.2025.
//

import SwiftUI

@MainActor
final class VideoListViewModel: ObservableObject {

    @Published var videos: [VideoItemModel]     = []
    @Published var errorMessage: String?        = nil
    @Published var isLoading                    = false
    @Published var filterText                   = ""
    @Published var videoToDelete: VideoItemModel?      = nil
    @Published var showDeleteConfirmation       = false
    @Published var selectedVideo: VideoItemModel? = nil

    // MARK: ‑ Filtering

    /// Видео по имени (A–Z), отфильтрованные по `filterText`: подстрока в любом месте
    /// имени, без учёта регистра и диакритики. Пустой фильтр — все видео.
    var filteredFiles: [VideoItemModel] {
        let query = filterText.trimmingCharacters(in: .whitespacesAndNewlines)
        let sorted = videos.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        guard !query.isEmpty else { return sorted }
        return sorted.filter { $0.name.localizedStandardContains(query) }
    }

    // MARK: ‑ Public API (вызывается View)

    func loadVideos(host: String, token: String) async {
        errorMessage = nil
        isLoading    = true
        defer { isLoading = false }

        switch await VideoService.shared.fetchFiles(host: host, token: token) {
        case .success(let fetched):
            videos = fetched
        case .failure(let error):
            errorMessage = (error as? LocalizedError)?.errorDescription ?? "Unknown error"
        }
    }

    func delete(_ file: VideoItemModel, host: String, token: String) async {
        switch await VideoService.shared.deleteFile(name: file.name, host: host, token: token) {
        case .success:
            videos.removeAll { $0 == file }
        case .failure(let error):
            errorMessage = "Delete failed: \(error.localizedDescription)"
        }
    }
}
