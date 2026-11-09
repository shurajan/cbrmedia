//
//  LivePlayerView.swift
//  CBRPlayer
//
//  Модальный плеер живого стрима (fullScreenCover поверх Watchlist): как плеер записей,
//  но без перемотки — плашка LIVE, пауза и возврат к живому краю.
//

import SwiftUI

struct LivePlayerView: View {
    @Environment(\.dismiss) private var dismiss

    @StateObject private var viewModel: LivePlayerViewModel
    private let session: SessionStorage

    init(user: String, session: SessionStorage) {
        _viewModel = StateObject(wrappedValue: LivePlayerViewModel(user: user))
        self.session = session
    }

    var body: some View {
        Group {
            if case .playing = viewModel.phase, let controller = viewModel.playerController {
                FSVideoPlayerView(
                    controller: controller,
                    live: .init(title: viewModel.user, joinedAt: viewModel.joinedAt)
                ) {
                    dismiss()
                }
                .ignoresSafeArea(edges: [.top])
            } else if case .unavailable(let title, let detail) = viewModel.phase {
                unavailable(title: title, detail: detail)
            } else {
                connecting
            }
        }
        .withPerformanceOverlay()
        .task { await viewModel.connect(host: session.host, token: session.token) }
        .onDisappear { viewModel.stop() }
    }
}

// MARK: - Subviews

private extension LivePlayerView {
    var connecting: some View {
        screen {
            ProgressView()
                .controlSize(.large)
                .tint(Theme.accent)
            Text("Connecting to \(viewModel.user)…")
                .foregroundStyle(Theme.textSecondary)
        }
    }

    func unavailable(title: String, detail: String) -> some View {
        screen {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 40))
                .foregroundStyle(Theme.warning)
            Text(title)
                .foregroundStyle(.white)
            Text(detail)
                .font(.subheadline)
                .foregroundStyle(Theme.textSecondary)
        }
    }

    /// Чёрный экран плеера с кнопкой «закрыть» и содержимым по центру.
    func screen(@ViewBuilder content: () -> some View) -> some View {
        ZStack {
            Color.black.ignoresSafeArea()

            VStack {
                HStack {
                    Button {
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 25, weight: .semibold))
                            .foregroundColor(.white)
                            .frame(width: 53, height: 53)
                            .contentShape(Circle())
                    }
                    .accessibilityLabel("Close")
                    Spacer()
                }
                .padding(.horizontal, 16)
                .padding(.top, 16)

                Spacer()

                VStack(spacing: 12) {
                    content()
                }
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)

                Spacer()
            }
        }
    }
}
