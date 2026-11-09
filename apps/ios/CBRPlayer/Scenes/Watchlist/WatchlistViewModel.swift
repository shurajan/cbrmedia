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
    /// Кого сейчас добавляем: сервер отвечает после первой проверки recorder'ом (5–8 с).
    @Published var checkingUser: String?
    @Published var entryToDelete: WatchEntryModel?
    /// Чей живой стрим открыт в модальном плеере.
    @Published var liveEntry: WatchEntryModel?
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

    /// Имя, которое будет добавлено: из поля ввода или из вставленной ссылки.
    var resolvedNewUser: String? { Self.userName(from: newUser) }

    /// Итоговое имя отличается от ввода (ссылка или заглавные буквы): показываем, кого добавим.
    var linkHint: String? {
        guard let user = resolvedNewUser, user != trimmedNewUser else { return nil }
        return user
    }

    var canAdd: Bool { resolvedNewUser != nil && !isSubmitting }

    /// Имя пользователя из ввода: имя как есть или ссылка на его страницу, где имя —
    /// последний непустой сегмент пути (`https://site/<user>`, `https://site/<раздел>/<user>/`).
    /// Схема необязательна, query и fragment отбрасываются, имя приводится к нижнему
    /// регистру (`Alice` и `alice` — один пользователь). Те же правила —
    /// `store.UserFromInput` в services/core; допустимость имени проверяет сервер.
    static func userName(from input: String) -> String? {
        let text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        guard text.contains("/") else { return text.lowercased() }

        let link = text.contains("://") ? text : "https://" + text
        guard let path = URLComponents(string: link)?.path else { return nil }
        return path.split(separator: "/").last.map { $0.lowercased() }
    }

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
        guard let user = resolvedNewUser else { return }
        let input = newUser
        checkingUser = user
        defer { checkingUser = nil }
        // Ответ приходит через 5–8 с; если за это время в поле начали вводить другое — не стираем.
        if await submit({ await self.service.add(user: user, host: host, token: token) }, host: host, token: token),
           newUser == input {
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

    /// Выполняет действие и перечитывает список — и при ошибке: пока сервер ждал проверку
    /// recorder'ом, фоновое обновление могло показать отклонённого пользователя.
    /// Возвращает true при успехе.
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
            await load(host: host, token: token, silent: true)
            return false
        }
    }
}
