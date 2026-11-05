//
//  VideoListView.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 24.04.2025.
//

import SwiftUI

struct VideoListView: View {
    @EnvironmentObject private var session: SessionStorage
    @EnvironmentObject private var globalSettings: GlobalSettings
    @Binding var navigationPath: [NavigationDestination]
    /// Меняется, когда в MainTabView нажали «обновить».
    var refreshSignal = 0
    
    @StateObject private var viewModel = VideoListViewModel()

    var body: some View {
        VStack {
            if viewModel.isLoading {
                ProgressView("Loading videos…")
                    .padding()
            } else if let message = viewModel.errorMessage {
                VStack(spacing: 16) {
                    Text(message)
                        .foregroundStyle(Theme.warning)
                        .multilineTextAlignment(.center)

                    Button(action: {
                        session.logout()
                        navigationPath = []
                    }) {
                        Text("Back to Login")
                            .font(.headline)
                            .foregroundColor(Theme.onPrimary)
                            .padding(.vertical, 10)
                            .padding(.horizontal, 20)
                            .background(Theme.primary)
                            .cornerRadius(12)
                    }
                }
                .frame(maxWidth: 500)
                .padding()
            } else {
                content
            }
        }
        .themedScreenBackground()
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: refreshSignal) { Task { await refresh() } }
        .alert("Delete Video?", isPresented: $viewModel.showDeleteConfirmation, presenting: viewModel.videoToDelete) { file in
            Button("Delete", role: .destructive) {
                Task { await delete(file) }
            }
            Button("Cancel", role: .cancel) { }
        } message: { file in
            Text("Are you sure you want to delete \"\(file.name)\"?")
        }
        .task { await initialLoad() }
        .fullScreenCover(item: $viewModel.selectedVideo) { video in
            VideoPlayerView(video: video, session: session)
                .environmentObject(globalSettings)
        }
    }
}

// MARK: - Private Subviews & Helpers

private extension VideoListView {
    var content: some View {
        VStack(spacing: 10) {
            filterField
                .padding(.horizontal)

            List {
                ForEach(viewModel.filteredFiles, id: \.id) { video in
                    VideoItemView(video: video) { video in
                        viewModel.selectedVideo = video
                    }
                    .swipeActions {
                        Button(role: .destructive) {
                            viewModel.videoToDelete = video
                            viewModel.showDeleteConfirmation = true
                        } label: {
                            Label("Delete", systemImage: "trash")
                        }
                        .tint(Theme.destructiveFill)
                    }
                    .listRowBackground(Theme.surface)
                    .listRowSeparatorTint(Theme.separator)
                }
            }
            .listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .overlay {
                // Фильтр ничего не нашёл: таблица пустая, подсказка — на фоне.
                if viewModel.filteredFiles.isEmpty, !viewModel.videos.isEmpty {
                    Text("Nothing found")
                        .font(.title3)
                        .foregroundStyle(Theme.textTertiary)
                        .allowsHitTesting(false)
                }
            }
            .refreshable { await refresh() }
        }
    }

    /// Фильтр по имени на месте прежнего переключателя сортировки.
    var filterField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(Theme.textSecondary)
            TextField("Filter by name", text: $viewModel.filterText)
                .autocorrectionDisabled(true)
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
            if !viewModel.filterText.isEmpty {
                Button {
                    viewModel.filterText = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(Theme.textSecondary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear filter")
            }
        }
        .themedField()
    }

    func initialLoad() async {
        guard let host = session.host, let token = session.token else { return }
        await viewModel.loadVideos(host: host, token: token)
    }
    
    func refresh() async {
        await initialLoad()
    }
    
    func delete(_ file: VideoItemModel) async {
        guard let host = session.host, let token = session.token else { return }
        await viewModel.delete(file, host: host, token: token)
    }
}
