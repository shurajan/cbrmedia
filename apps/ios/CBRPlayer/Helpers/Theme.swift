//
//  Theme.swift
//  CBRPlayer
//
//  Палитра приложения и её роли. Приложение всегда в тёмной теме
//  (INFOPLIST_KEY_UIUserInterfaceStyle = Dark). В экранах используются только
//  роли Theme; Palette — сырьё для ролей.
//

import SwiftUI

/// Тональные шкалы семи исходных цветов: 50 — самый светлый, 950 — самый тёмный.
/// Построены в OKLCH: тон каждого цвета сохраняется, светлота шагов равномерна для
/// глаза (50 ≈ 0.97 … 950 ≈ 0.22), насыщенность убывает к краям. Исходный цвет
/// входит в шкалу без изменений на ближайшем по светлоте шаге.
enum Palette {
    // Ocean — #0C6A93 (исходный цвет — шаг 700)
    static let ocean50 = Color(hex: 0xEBF7FF)
    static let ocean100 = Color(hex: 0xD0EDFF)
    static let ocean200 = Color(hex: 0xABD8F4)
    static let ocean300 = Color(hex: 0x8BBFDE)
    static let ocean400 = Color(hex: 0x6CA7C9)
    static let ocean500 = Color(hex: 0x4C8FB3)
    static let ocean600 = Color(hex: 0x29779F)
    static let ocean700 = Color(hex: 0x0C6A93) // исходный
    static let ocean800 = Color(hex: 0x004867)
    static let ocean900 = Color(hex: 0x003045)
    static let ocean950 = Color(hex: 0x001E2D)

    // Sky — #2F85AC (исходный цвет — шаг 500)
    static let sky50 = Color(hex: 0xEAF8FF)
    static let sky100 = Color(hex: 0xCEEDFF)
    static let sky200 = Color(hex: 0xA0DAF9)
    static let sky300 = Color(hex: 0x81C1E3)
    static let sky400 = Color(hex: 0x61A8CC)
    static let sky500 = Color(hex: 0x2F85AC) // исходный
    static let sky600 = Color(hex: 0x25789C)
    static let sky700 = Color(hex: 0x136080)
    static let sky800 = Color(hex: 0x004964)
    static let sky900 = Color(hex: 0x023043)
    static let sky950 = Color(hex: 0x011E2B)

    // Deep — #113844 (исходный цвет — шаг 900)
    static let deep50 = Color(hex: 0xE7F9FF)
    static let deep100 = Color(hex: 0xD7ECF4)
    static let deep200 = Color(hex: 0xBFD5DE)
    static let deep300 = Color(hex: 0xA3BCC5)
    static let deep400 = Color(hex: 0x89A4AD)
    static let deep500 = Color(hex: 0x6F8C96)
    static let deep600 = Color(hex: 0x55747F)
    static let deep700 = Color(hex: 0x3D5E69)
    static let deep800 = Color(hex: 0x244854)
    static let deep900 = Color(hex: 0x113844) // исходный
    static let deep950 = Color(hex: 0x021E27)

    // Orange — #F47321 (исходный цвет — шаг 400)
    static let orange50 = Color(hex: 0xFFF2EC)
    static let orange100 = Color(hex: 0xFFE1D2)
    static let orange200 = Color(hex: 0xFFC1A2)
    static let orange300 = Color(hex: 0xFF9A66)
    static let orange400 = Color(hex: 0xF47321) // исходный
    static let orange500 = Color(hex: 0xD45E0A)
    static let orange600 = Color(hex: 0xB14C01)
    static let orange700 = Color(hex: 0x8E3C01)
    static let orange800 = Color(hex: 0x6D2C00)
    static let orange900 = Color(hex: 0x4A1B00)
    static let orange950 = Color(hex: 0x301000)

    // Yellow — #F0BD22 (исходный цвет — шаг 200)
    static let yellow50 = Color(hex: 0xFFF4DB)
    static let yellow100 = Color(hex: 0xFFE5A8)
    static let yellow200 = Color(hex: 0xF0BD22) // исходный
    static let yellow300 = Color(hex: 0xE1B014)
    static let yellow400 = Color(hex: 0xC39804)
    static let yellow500 = Color(hex: 0xA68003)
    static let yellow600 = Color(hex: 0x896A04)
    static let yellow700 = Color(hex: 0x6E5401)
    static let yellow800 = Color(hex: 0x543F00)
    static let yellow900 = Color(hex: 0x372901)
    static let yellow950 = Color(hex: 0x231900)

    // Green — #34C26B (исходный цвет — шаг 400): привычный цвет воспроизведения
    static let green50 = Color(hex: 0xE9FBEF)
    static let green100 = Color(hex: 0xCBF5D9)
    static let green200 = Color(hex: 0x9BE8B6)
    static let green300 = Color(hex: 0x6AD892)
    static let green400 = Color(hex: 0x34C26B) // исходный
    static let green500 = Color(hex: 0x1FA858)
    static let green600 = Color(hex: 0x178A48)
    static let green700 = Color(hex: 0x126E3A)
    static let green800 = Color(hex: 0x0C522B)
    static let green900 = Color(hex: 0x07371D)
    static let green950 = Color(hex: 0x042412)

    // Red — #FF6B6B (исходный цвет — шаг 400): удаление и идущая запись
    static let red50 = Color(hex: 0xFFF0F0)
    static let red100 = Color(hex: 0xFFDCDC)
    static let red200 = Color(hex: 0xFFB8B8)
    static let red300 = Color(hex: 0xFF8F8F)
    static let red400 = Color(hex: 0xFF6B6B) // исходный
    static let red500 = Color(hex: 0xE84F52)
    static let red600 = Color(hex: 0xCC3B3F)
    static let red700 = Color(hex: 0xA62E31)
    static let red800 = Color(hex: 0x7F2124)
    static let red900 = Color(hex: 0x561517)
    static let red950 = Color(hex: 0x380C0E)
}

/// Роли цветов. В комментариях — контраст по WCAG к фону, на котором роль
/// используется (текст ≥ 4.5, иконки и крупные элементы ≥ 3).
enum Theme {
    // MARK: Поверхности

    /// Фон экранов, панели навигации.
    static let background = Palette.deep950
    /// Карточки, строки списков, поля ввода, форма настроек (1.4 к фону).
    static let surface = Palette.deep900
    /// Поверхность над surface: выделение, вложенные панели (1.3 к surface).
    static let surfaceElevated = Palette.deep800
    /// Разделители строк.
    static let separator = Palette.deep700
    /// Рамка полей ввода.
    static let border = Palette.sky700

    // MARK: Текст

    /// Основной текст (15.9 на фоне, 11.6 на surface).
    static let textPrimary = Palette.deep50
    /// Подписи и метаданные (6.3 на surface).
    static let textSecondary = Palette.deep300
    /// Неактивное и наименее важное (3.5 на surface).
    static let textTertiary = Palette.deep500

    // MARK: Акцент и действия

    /// Текст и иконки интерактивных элементов, системный AccentColor (4.8 на surface).
    static let accent = Palette.sky400
    /// Заливки акцентом без текста: дорожка переключателя.
    static let accentFill = Palette.sky500
    /// Акцент в нажатом состоянии.
    static let accentPressed = Palette.sky300

    /// Заливка основных кнопок.
    static let primary = Palette.ocean700
    /// Заливка основной кнопки в нажатом состоянии.
    static let primaryPressed = Palette.ocean800
    /// Текст на primary (6.0).
    static let onPrimary = Color.white

    /// Кнопка play в списке видео и у public-строк Watchlist (7.4 на фоне, 5.5 на surface).
    static let play = Palette.green400
    /// Прогресс и бегунок плеера (6.0 на фоне).
    static let playback = Palette.orange400

    /// Удаление: текст и иконки (6.2 на фоне, 4.6 на surface).
    static let destructive = Palette.red400
    /// Заливка удаления (свайп) под белым текстом (5.2).
    static let destructiveFill = Palette.red600

    // MARK: Состояния

    /// Идёт запись: статус в списке слежения и сводка на главной (4.6 на surface), точка плашки LIVE в живом плеере.
    static let recording = Palette.red400
    /// Ошибки и предупреждения: текст и значки (9.9 на фоне).
    static let warning = Palette.yellow200
    /// Текст на заливке warning (9.9).
    static let onWarning = Palette.deep950
    /// Информационные сообщения.
    static let info = Palette.sky400
}

extension Color {
    /// Цвет из 0xRRGGBB.
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }
}

extension View {
    /// Поле ввода той же формы, что `.textFieldStyle(.roundedBorder)`, но в цветах палитры:
    /// системный стиль в тёмной теме заливает поле чистым чёрным.
    func themedField() -> some View {
        padding(.horizontal, 8)
            .padding(.vertical, 7)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(Theme.border, lineWidth: 1))
            .foregroundStyle(Theme.textPrimary)
    }

    /// Фон экрана на всю площадь, включая безопасные зоны.
    func themedScreenBackground() -> some View {
        frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Theme.background.ignoresSafeArea())
    }
}
