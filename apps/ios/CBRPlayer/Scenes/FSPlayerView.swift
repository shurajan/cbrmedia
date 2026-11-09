//
//  FSPlayerView.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 16.04.2025.
//

import SwiftUI

/// Экраны NavigationStack: вход → главная с плитками → дочерние экраны разделов.
enum NavigationDestination: Hashable {
    case login
    case main
    case videos
    case watchlist
}

struct FSPlayerView: View {
    @State private var navigationPath = [NavigationDestination]()
    @StateObject private var session = SessionStorage()
    @StateObject private var globalSettings = GlobalSettings()
    
    var body: some View {
        NavigationStack(path: $navigationPath) {
            LoginView(navigationPath: $navigationPath)
                .environmentObject(session)
                .navigationDestination(for: NavigationDestination.self) { destination in
                    switch destination {
                    case .login:
                        LoginView(navigationPath: $navigationPath)
                            .environmentObject(session)
                    case .main:
                        MainView()
                            .environmentObject(session)
                            .environmentObject(globalSettings)
                    case .videos:
                        VideoListView(navigationPath: $navigationPath)
                            .environmentObject(session)
                            .environmentObject(globalSettings)
                    case .watchlist:
                        WatchlistView()
                            .environmentObject(session)
                            .environmentObject(globalSettings)
                    }
                }
        }
        .tint(Theme.accent)
    }
}
