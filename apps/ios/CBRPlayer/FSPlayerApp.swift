//
//  FSPlayerApp.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 16.04.2025.
//

import SwiftUI

@main
struct FSPlayerApp: App {
    
    init() {
        let appearance = UINavigationBarAppearance()
        appearance.configureWithOpaqueBackground()
        appearance.backgroundColor = UIColor(Theme.background)
        appearance.shadowColor = .clear
        appearance.titleTextAttributes = [
            .foregroundColor: UIColor(Theme.textPrimary)
        ]
        appearance.largeTitleTextAttributes = [
            .foregroundColor: UIColor(Theme.textPrimary)
        ]

        UINavigationBar.appearance().standardAppearance = appearance
        UINavigationBar.appearance().scrollEdgeAppearance = appearance
        UINavigationBar.appearance().compactAppearance = appearance

        UINavigationBar.appearance().tintColor = UIColor(Theme.accent)
    }
    
    var body: some Scene {
        WindowGroup {
            FSPlayerView()
        }
    }
}
