//
//  HomeTileView.swift
//  CBRPlayer
//
//  Плитка раздела на главной: иконка в круге фона (как кнопка play), название,
//  описание и сводка. `alert` — часть сводки, требующая внимания (идущая запись),
//  выводится цветом recording с точкой.
//

import SwiftUI

struct HomeTileView: View {
    let icon: String
    let title: String
    let subtitle: String
    var summary: String?
    var alert: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top) {
                Image(systemName: icon)
                    .font(.system(size: 28, weight: .regular))
                    .foregroundStyle(Theme.accent)
                    .frame(width: 60, height: 60)
                    .background(Theme.background, in: Circle())
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(Theme.textTertiary)
            }

            Spacer(minLength: 16)

            VStack(alignment: .leading, spacing: 6) {
                Text(title)
                    .font(.title2.weight(.semibold))
                    .foregroundStyle(Theme.textPrimary)
                Text(subtitle)
                    .font(.subheadline)
                    .foregroundStyle(Theme.textSecondary)
                summaryLine
                    .font(.footnote)
                    .padding(.top, 6)
            }
        }
        .padding(24)
        .frame(maxWidth: .infinity, minHeight: 232, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: 20))
        .contentShape(RoundedRectangle(cornerRadius: 20))
        .accessibilityElement(children: .combine)
    }

    /// Сводка одной строкой; без данных — пустая строка той же высоты, чтобы плитки не прыгали.
    private var summaryLine: some View {
        HStack(spacing: 6) {
            if let summary {
                Text(alert == nil ? summary : "\(summary) ·")
                    .foregroundStyle(Theme.textTertiary)
            }
            if let alert {
                Circle()
                    .fill(Theme.recording)
                    .frame(width: 8, height: 8)
                Text(alert)
                    .foregroundStyle(Theme.recording)
            }
            if summary == nil && alert == nil {
                Text(" ")
            }
        }
    }
}
