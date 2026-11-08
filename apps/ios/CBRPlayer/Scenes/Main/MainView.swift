//
//  MainView.swift
//  CBRPlayer
//
//  Главная после входа: плитки разделов вместо вкладок. Плитка открывает дочерний
//  экран (push в NavigationStack), системная кнопка «назад» на нём возвращает сюда.
//  Набор плиток фиксированный — новая появляется вместе с новой функцией.
//  Общие кнопки: назад к входу и настройки. Контент без ScrollView: набор плиток
//  мал и помещается целиком, а крупный заголовок «Home» в iOS 26 внутри ScrollView
//  не отрисовывался. Под плитками — сводка по разделу
//  (число видео и объём, число пользователей и идущие записи), загружается при
//  каждом показе главной; если загрузить не удалось, плитка остаётся без сводки.
//

import SwiftUI

struct MainView: View {
    @EnvironmentObject private var session: SessionStorage
    @EnvironmentObject private var globalSettings: GlobalSettings
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel = HomeViewModel()
    @State private var showSettings = false

    private let columns = Array(repeating: GridItem(.flexible(), spacing: 24), count: 3)

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 32) {
                if let host = session.host {
                    Text("Connected to \(host)")
                        .font(.subheadline)
                        .foregroundStyle(Theme.textTertiary)
                }

                LazyVGrid(columns: columns, alignment: .leading, spacing: 24) {
                    NavigationLink(value: NavigationDestination.videos) {
                        HomeTileView(
                            icon: "play.rectangle",
                            title: "Videos",
                            subtitle: "Recorded streams, ready to play",
                            summary: viewModel.videosSummary
                        )
                    }
                    NavigationLink(value: NavigationDestination.watchlist) {
                        HomeTileView(
                            icon: "eye",
                            title: "Watchlist",
                            subtitle: "Who the recorder follows",
                            summary: viewModel.watchlistSummary,
                            alert: viewModel.recordingSummary
                        )
                    }
                }
                .buttonStyle(.plain)
            }
            .frame(maxWidth: 960, alignment: .leading)
            .padding(.horizontal, 48)
            .padding(.top, 8)
            .padding(.bottom, 48)
            .frame(maxWidth: .infinity)
            Spacer(minLength: 0)
        }
        .themedScreenBackground()
        .navigationTitle("Home")
        .navigationBarTitleDisplayMode(.large)
        .navigationBarBackButtonHidden(true)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button {
                    dismiss()
                } label: {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundColor(Theme.accent)
                }
                .accessibilityLabel("Back to login")
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    showSettings = true
                } label: {
                    Image(systemName: "gearshape")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundColor(Theme.accent)
                }
                .accessibilityLabel("Settings")
            }
        }
        .sheet(isPresented: $showSettings) {
            SettingsView()
                .environmentObject(globalSettings)
        }
        .onAppear {
            // Срабатывает и при возврате с дочернего экрана: сводка обновляется.
            Task { await load() }
        }
    }

    private func load() async {
        guard let host = session.host, let token = session.token else { return }
        await viewModel.load(host: host, token: token)
    }
}
