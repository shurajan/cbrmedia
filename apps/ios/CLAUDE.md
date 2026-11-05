# CBRPlayer (iOS / iPadOS)

SwiftUI-клиент службы `services/api`: логин по паролю, список видео, HLS-плеер с сохранением позиции.

- Xcode-проект `CBRPlayer.xcodeproj`, схема **CBRPlayer**, только iPad (`TARGETED_DEVICE_FAMILY = 2`), iOS 26.6, Swift 6, bundle `com.bralnin.cbrplayer`.
- Без внешних SPM-зависимостей. Info.plist генерируется (`GENERATE_INFOPLIST_FILE`, ключи `INFOPLIST_KEY_*` в pbxproj).
- Группы — file-system synchronized (objectVersion 77): новый `.swift` в `CBRPlayer/` попадает в таргет автоматически, pbxproj править не нужно.
- Сборка из CLI: `xcodebuild -project CBRPlayer.xcodeproj -scheme CBRPlayer -destination 'platform=iOS Simulator,name=iPad Pro 13-inch (M5)' build`
  (нужен симулятор iOS ≥ 26.6).
- Тесты — шаблонные заглушки (`CBRPlayerTests`, `CBRPlayerUITests`); deployment target 26.6 у проекта и всех таргетов —
  менять синхронно, иначе тесты не соберутся. Исторические имена `FS*` (от FSPlayer) сохранены.

## Структура `CBRPlayer/`

| Путь | Что |
|------|-----|
| `FSPlayerApp.swift` | `@main`, настройка UINavigationBar |
| `Scenes/FSPlayerView.swift` | корень: `NavigationStack` по `NavigationDestination {login, videoList}`, держит `SessionStorage`, `GlobalSettings` |
| `Scenes/LoginView/` | `LoginView`, `LoginViewModel` (host + пароль → `AuthService.login`), `HostAndPortInputView` |
| `Scenes/VideoList/` | `VideoListView`, `VideoListViewModel` (загрузка, сортировка `SortOption`, удаление с подтверждением), `VideoItemView` |
| `Scenes/VideoView/` | `VideoPlayerView`, `VideoPlayerViewModel` — `AVURLAsset` с заголовком `Authorization` через `AVURLAssetHTTPHeaderFieldsKey`, URL `http://host + hlsURL`, позиция сохраняется на паузе, сбрасывается в конце |
| `Scenes/Settings/` | `SettingsView`, `GlobalSettings` (оверлей производительности), `PerfomanceMonitor/` |
| `Controls/FSVideoPlayer/` | свой плеер: `FSPlayerController` (play/pause/seek/scrub, очередь seek), `FSVideoPlayerLayerView`, `FSVideoPlayerView`, `FSVideoScrubber` |
| `Services/` | `AuthService`, `VideoService` (singletons `@MainActor`, `Result<_, Error>`), `NetworkServiceError`, `KeychainTokenService` (токен), `UserDataStorageService` (UserDefaults: host, позиции видео), `SessionStorage` (`ObservableObject`: token, host, logout) |
| `Models/VideoItemModel.swift` | DTO `GET /videos` — должен совпадать с Go `MediaFile` в `services/api/internal/httpapi/videos.go` |
| `Helpers/Theme.swift` | палитра `Palette` и роли `Theme` (см. ниже), `Color(hex:)`, модификаторы `themedScreenBackground()`, `themedField()` |
| `Helpers/View+Extension.swift` | оверлей производительности |

## Оформление

Приложение всегда в тёмной теме (`INFOPLIST_KEY_UIUserInterfaceStyle = Dark`), системная светлая тема не учитывается.
В экранах — только роли `Theme`; `Palette` — сырьё для ролей. Без `.red`/`.green`/системных фонов.

**Палитра** (`Palette` в `Helpers/Theme.swift`): пять исходных цветов, у каждого тональная шкала 50…950, построенная в OKLCH
(тон сохраняется, светлота равномерна для глаза). Исходный цвет стоит в шкале без изменений:
`ocean700` = `#0c6a93`, `sky500` = `#2f85ac`, `deep900` = `#113844`, `orange400` = `#f47321`, `yellow200` = `#f0bd22`.

**Роли** (`Theme`), контраст по WCAG — текст ≥ 4.5, иконки ≥ 3:

| Роль | Цвет | Где |
|---|---|---|
| `background` | deep950 | фон экранов, панель навигации, подложка кнопки play |
| `surface` / `surfaceElevated` | deep900 / deep800 | карточки и строки, поля, форма настроек, фон сегментов / выделение |
| `separator` / `border` | deep700 / sky700 | разделители строк / рамка полей |
| `textPrimary` / `textSecondary` / `textTertiary` | deep50 / deep300 / deep500 | основной текст / метаданные / неактивное |
| `accent` | sky400 (также `AccentColor`) | текст и иконки интерактивных элементов (4.8 на surface) |
| `accentFill` / `accentPressed` | sky500 / sky300 | дорожка переключателя / нажатое состояние |
| `primary` / `primaryPressed` / `onPrimary` | ocean700 / ocean800 / белый | заливка кнопок Login, Back to Login, выбранного сегмента |
| `playback` | orange400 | иконка play, прогресс и бегунок плеера |
| `destructive` / `destructiveFill` | orange400 / orange600 | удаление: текст / заливка свайпа под белым (5.4) |
| `warning` / `onWarning` | yellow200 / deep950 | тексты ошибок, значок ошибки в плеере, оверлей производительности |
| `info` | sky400 | информационные сообщения |

Зелёного в палитре нет, поэтому роли `success` нет — при необходимости согласовать цвет.
Глобальные цвета UIKit (панель навигации, `UISegmentedControl`) — в `FSPlayerApp.init`. Плеер чёрный под видео с белыми элементами.
Поля ввода — модификатор `themedField()` (системный `.roundedBorder` в тёмной теме заливает чёрным).

## Соглашения

- MVVM: `ObservableObject` + `@Published`, view model'и `@MainActor`; DI через `environmentObject`.
- Сеть — `URLSession` async/await, plain HTTP на `host:port`, ошибки маппятся в `NetworkServiceError`.
