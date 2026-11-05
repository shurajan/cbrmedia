//
//  WatchlistViewModel.swift
//  CBRPlayer
//
//  Список пользователей, за которыми следит recorder: добавление, удаление,
//  повторное включение выключенных. Состояние берётся из services/api.
//

import SwiftUI

@MainActor
final class WatchlistViewModel: ObservableObject {

    @Published var entries: [WatchEntryModel] = []
    /// Ошибка загрузки списка — показывается вместо таблицы.
    @Published var loadError: String?
    /// Ошибка добавления/удаления — показывается под полем ввода.
    @Published var actionError: String?
    @Published var isLoading = false
    @Published var isSubmitting = false
    @Published var newUser = ""
    @Published var entryToDelete: WatchEntryModel?
    @Published var showDeleteConfirmation = false

    /// Как часто обновлять статусы, пока экран открыт.
    static let pollInterval: Duration = .seconds(10)

    private let service: WatchlistServiceProtocol

    init(service: WatchlistServiceProtocol? = nil) {
        self.service = service ?? WatchlistService.shared
    }

    /// Имя из поля ввода без пробелов по краям.
    var trimmedNewUser: String {
        newUser.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var canAdd: Bool { !trimmedNewUser.isEmpty && !isSubmitting }

    /// Загружает список. `silent` — без индикатора загрузки (фоновое обновление статусов).
    func load(host: String, token: String, silent: Bool = false) async {
        if !silent { isLoading = true }
        defer { if !silent { isLoading = false } }

        switch await service.fetch(host: host, token: token) {
        case .success(let fetched):
            // Без изменений не трогаем список: перерисовка закрыла бы открытое свайп-меню.
            if fetched != entries { entries = fetched }
            if loadError != nil { loadError = nil }
        case .failure(let error):
            // Сбой фонового обновления не прячет уже показанный список.
            if !silent || entries.isEmpty {
                loadError = error.localizedDescription
            }
        }
    }

    /// Обновляет статусы каждые `pollInterval`, пока задача не отменена (экран закрыт).
    func poll(host: String, token: String) async {
        while !Task.isCancelled {
            guard (try? await Task.sleep(for: Self.pollInterval)) != nil else { return }
            await load(host: host, token: token, silent: true)
        }
    }

    func addNewUser(host: String, token: String) async {
        let user = trimmedNewUser
        guard !user.isEmpty else { return }
        if await submit({ await self.service.add(user: user, host: host, token: token) }, host: host, token: token) {
            newUser = ""
        }
    }

    /// Снова включает пользователя, выключенного recorder'ом (status API ответил 4xx).
    func enable(_ entry: WatchEntryModel, host: String, token: String) async {
        _ = await submit({ await self.service.add(user: entry.user, host: host, token: token) }, host: host, token: token)
    }

    func delete(_ entry: WatchEntryModel, host: String, token: String) async {
        if await submit({ await self.service.remove(user: entry.user, host: host, token: token) }, host: host, token: token) {
            entries.removeAll { $0.user == entry.user }
        }
    }

    /// Выполняет действие, по успеху перечитывает список. Возвращает true при успехе.
    private func submit(_ action: () async -> Result<Void, Error>, host: String, token: String) async -> Bool {
        actionError = nil
        isSubmitting = true
        defer { isSubmitting = false }

        switch await action() {
        case .success:
            await load(host: host, token: token, silent: true)
            return true
        case .failure(let error):
            actionError = error.localizedDescription
            return false
        }
    }
}
