//
//  MainTabView.swift
//  CBRPlayer
//
//  Экран после входа: вкладки «Videos» (медиатека) и «Watchlist» (кого записывать).
//  Переключатель вкладок — классическая нижняя панель с иконками: на iPad её даёт
//  компактный горизонтальный размер у TabView (иначе вкладки рисуются сверху);
//  содержимому вкладок возвращается обычный размер, чтобы не менялась вёрстка.
//  Общие кнопки: назад к входу, обновить открытую вкладку, настройки. Кнопки,
//  объявленные внутри вкладки, до панели навигации через TabView не доходят,
//  поэтому обновление передаётся во вкладку счётчиком `refreshSignal`.
//

import SwiftUI

struct MainTabView: View {
    @EnvironmentObject private var globalSettings: GlobalSettings
    @Environment(\.dismiss) private var dismiss
    @Binding var navigationPath: [NavigationDestination]

    /// Размер, который был до подмены на .compact для TabView.
    @Environment(\.horizontalSizeClass) private var regularSizeClass
    @State private var selection: MainTab = .videos
    @State private var showSettings = false
    @State private var videosRefresh = 0
    @State private var watchlistRefresh = 0

    enum MainTab: Hashable {
        case videos
        case watchlist
    }

    var body: some View {
        TabView(selection: $selection) {
            Tab("Videos", systemImage: "play.rectangle", value: MainTab.videos) {
                VideoListView(navigationPath: $navigationPath, refreshSignal: videosRefresh)
                    .environment(\.horizontalSizeClass, regularSizeClass)
            }
            Tab("Watchlist", systemImage: "eye", value: MainTab.watchlist) {
                WatchlistView(refreshSignal: watchlistRefresh)
                    .environment(\.horizontalSizeClass, regularSizeClass)
            }
        }
        .environment(\.horizontalSizeClass, .compact)
        .tint(Theme.accent)
        .navigationBarTitleDisplayMode(.inline)
        .navigationBarBackButtonHidden(true)
        .toolbar {
            ToolbarItem(placement: .navigationBarLeading) {
                Button {
                    dismiss()
                } label: {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundColor(Theme.accent)
                }
            }
            ToolbarItem(placement: .navigationBarTrailing) {
                Button {
                    switch selection {
                    case .videos: videosRefresh += 1
                    case .watchlist: watchlistRefresh += 1
                    }
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundColor(Theme.accent)
                }
                .accessibilityLabel("Refresh")
            }
            ToolbarItem(placement: .navigationBarTrailing) {
                Button {
                    showSettings = true
                } label: {
                    Image(systemName: "gearshape")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundColor(Theme.accent)
                }
            }
        }
        .sheet(isPresented: $showSettings) {
            SettingsView()
                .environmentObject(globalSettings)
        }
    }
}
