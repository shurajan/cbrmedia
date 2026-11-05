//
//  WatchlistView.swift
//  CBRPlayer
//
//  Вкладка «Watchlist»: кого recorder отслеживает и записывает.
//

import SwiftUI

struct WatchlistView: View {
    @EnvironmentObject private var session: SessionStorage
    @StateObject private var viewModel = WatchlistViewModel()
    /// Меняется, когда в MainTabView нажали «обновить».
    var refreshSignal = 0

    var body: some View {
        VStack(spacing: 10) {
            addField
                .padding(.horizontal)

            if let error = viewModel.actionError {
                Text(error)
                    .font(.subheadline)
                    .foregroundStyle(Theme.warning)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal)
            }

            if viewModel.isLoading && viewModel.entries.isEmpty {
                ProgressView("Loading watchlist…")
                    .tint(Theme.accent)
                    .padding()
                Spacer()
            } else if let error = viewModel.loadError {
                Text(error)
                    .foregroundStyle(Theme.warning)
                    .multilineTextAlignment(.center)
                    .padding()
                Spacer()
            } else {
                list
            }
        }
        .themedScreenBackground()
        .onChange(of: refreshSignal) { Task { await load() } }
        .onChange(of: viewModel.newUser) { viewModel.actionError = nil }
        .alert("Remove from Watchlist?", isPresented: $viewModel.showDeleteConfirmation, presenting: viewModel.entryToDelete) { entry in
            Button("Remove", role: .destructive) {
                Task { await withSession { host, token in await viewModel.delete(entry, host: host, token: token) } }
            }
            Button("Cancel", role: .cancel) { }
        } message: { entry in
            Text("Recorder will stop watching \"\(entry.user)\". Recordings already made are kept.")
        }
        .task {
            await load()
            await withSession { host, token in await viewModel.poll(host: host, token: token) }
        }
    }
}

// MARK: - Subviews & Helpers

private extension WatchlistView {
    var addField: some View {
        HStack(spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "person.badge.plus")
                    .foregroundStyle(Theme.textSecondary)
                TextField("Add user to watch", text: $viewModel.newUser)
                    .autocorrectionDisabled(true)
                    .textInputAutocapitalization(.never)
                    .submitLabel(.done)
                    .onSubmit { Task { await add() } }
            }
            .themedField()

            Button {
                Task { await add() }
            } label: {
                if viewModel.isSubmitting {
                    ProgressView()
                        .tint(Theme.onPrimary)
                } else {
                    Text("Add")
                        .foregroundColor(Theme.onPrimary)
                }
            }
            .buttonStyle(.borderedProminent)
            .tint(Theme.primary)
            .disabled(!viewModel.canAdd)
        }
    }

    var list: some View {
        List {
            ForEach(viewModel.entries) { entry in
                WatchEntryView(entry: entry, isBusy: viewModel.isSubmitting) {
                    Task { await withSession { host, token in await viewModel.enable(entry, host: host, token: token) } }
                }
                    .swipeActions(edge: .trailing) {
                        Button(role: .destructive) {
                            viewModel.entryToDelete = entry
                            viewModel.showDeleteConfirmation = true
                        } label: {
                            Label("Remove", systemImage: "trash")
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
            if viewModel.entries.isEmpty {
                Text("No users yet")
                    .font(.title3)
                    .foregroundStyle(Theme.textTertiary)
                    .allowsHitTesting(false)
            }
        }
        .refreshable { await load() }
    }

    func load() async {
        await withSession { host, token in await viewModel.load(host: host, token: token) }
    }

    func add() async {
        await withSession { host, token in await viewModel.addNewUser(host: host, token: token) }
    }

    func withSession(_ action: (String, String) async -> Void) async {
        guard let host = session.host, let token = session.token else { return }
        await action(host, token)
    }
}
