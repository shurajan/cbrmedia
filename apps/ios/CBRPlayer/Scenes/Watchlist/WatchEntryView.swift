//
//  WatchEntryView.swift
//  CBRPlayer
//
//  Строка списка слежения: имя, статус от recorder, кто и когда добавил.
//

import SwiftUI

struct WatchEntryView: View {
    let entry: WatchEntryModel
    var isBusy = false
    /// Снова включить пользователя; кнопка показывается только у выключенных.
    var onEnable: () -> Void = {}

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: statusIcon)
                .font(.system(size: 22))
                .foregroundStyle(statusColor)
                .frame(width: 32)

            VStack(alignment: .leading, spacing: 6) {
                Text(entry.user)
                    .font(.headline)
                    .foregroundStyle(Theme.textPrimary)
                    .lineLimit(1)

                Text(statusText)
                    .font(.subheadline)
                    .foregroundStyle(statusColor)
                    .lineLimit(2)

                if let added = addedText {
                    Label(added, systemImage: "person.badge.plus")
                        .font(.caption2)
                        .foregroundStyle(Theme.textSecondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            if !entry.enabled {
                Button(action: onEnable) {
                    Label("Enable", systemImage: "play.fill")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Theme.onPrimary)
                }
                .buttonStyle(.borderedProminent)
                .tint(Theme.primary)
                .disabled(isBusy)
            }
        }
        .padding(.vertical, 8)
        .padding(.horizontal, 12)
        .background(Theme.surface)
        .cornerRadius(12)
    }

    private var statusIcon: String {
        switch entry.status {
        case .recording: "record.circle"
        case .watching: "eye"
        case .blocked: "nosign"
        case .notFound: "questionmark.circle"
        case .waiting: "hourglass"
        case .disabled: "pause.circle"
        }
    }

    private var statusColor: Color {
        switch entry.status {
        case .recording: Theme.recording
        case .watching: Theme.accent
        case .blocked: Theme.warning
        case .notFound: Theme.textSecondary
        case .waiting: Theme.textTertiary
        case .disabled: Theme.warning
        }
    }

    private var statusText: String {
        switch entry.status {
        case .recording(let room):
            room.isEmpty ? "Recording" : "Recording · \(room)"
        case .watching(let room):
            room.isEmpty ? "Watching" : "Watching · \(room)"
        case .blocked:
            "Access denied · checking daily"
        case .notFound:
            "Not found · checking daily"
        case .waiting:
            "Waiting for recorder"
        case .disabled(let reason):
            reason.isEmpty ? "Disabled" : "Disabled: \(reason)"
        }
    }

    private var addedText: String? {
        guard let date = entry.addedDate else { return nil }
        let when = date.formatted(date: .numeric, time: .shortened)
        guard let by = entry.addedBy, !by.isEmpty else { return "Added \(when)" }
        return "Added \(when) via \(by)"
    }
}
