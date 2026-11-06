# CBRPlayer (iOS / iPadOS)

SwiftUI-клиент службы `services/api`: логин по паролю, вкладки «Videos» (медиатека, HLS-плеер с сохранением позиции)
и «Watchlist» (кого recorder отслеживает и записывает).

- Xcode-проект `CBRPlayer.xcodeproj`, схема **CBRPlayer**, только iPad (`TARGETED_DEVICE_FAMILY = 2`), iOS 26.6, Swift 6, bundle `com.bralnin.cbrplayer`.
- Без внешних SPM-зависимостей. Info.plist генерируется (`GENERATE_INFOPLIST_FILE`, ключи `INFOPLIST_KEY_*` в pbxproj):
  отображаемое имя `cbr` (`CFBundleDisplayName`), категория `public.app-category.video`, всегда тёмная тема (`UIUserInterfaceStyle = Dark`).
- Иконка — `Assets.xcassets/AppIcon.appiconset/CBR_AppIcon_1024.png` (один размер 1024, остальные Xcode генерирует).
- Группы — file-system synchronized (objectVersion 77): новый `.swift` в `CBRPlayer/` попадает в таргет автоматически, pbxproj править не нужно.
- Сборка из CLI: `xcodebuild -project CBRPlayer.xcodeproj -scheme CBRPlayer -destination 'platform=iOS Simulator,name=iPad Pro 13-inch (M5)' build`
  (нужен симулятор iOS ≥ 26.6).
- Тесты (Swift Testing): `CBRPlayerTests/VideoListFilterTests` — фильтр видео, `WatchlistTests` — модель `/watchlist` и
  `WatchlistViewModel` на фейковом сервисе; UI-тесты — шаблонные заглушки. Deployment target 26.6 у проекта и всех таргетов —
  менять синхронно, иначе тесты не соберутся. Исторические имена `FS*` (от FSPlayer) сохранены.

## Структура `CBRPlayer/`

| Путь | Что |
|------|-----|
| `FSPlayerApp.swift` | `@main`, настройка UINavigationBar |
| `Scenes/FSPlayerView.swift` | корень: `NavigationStack` по `NavigationDestination {login, main}`, держит `SessionStorage`, `GlobalSettings` |
| `Scenes/Main/MainTabView.swift` | после входа: `TabView` «Videos» / «Watchlist» с нижней панелью иконок без подписей (названия — в `accessibilityLabel`; на iPad — через `.horizontalSizeClass = .compact` у `TabView`, содержимому вкладок возвращается исходный размер; без этого вкладки сверху); общие кнопки назад, обновить (открытую вкладку — через `refreshSignal`), настройки. Кнопки `.toolbar` внутри вкладки через `TabView` не доходят — добавлять сюда |
| `Scenes/LoginView/` | `LoginView`, `LoginViewModel` (host + пароль → `AuthService.login`), `HostAndPortInputView` |
| `Scenes/VideoList/` | `VideoListView` (поле фильтра над таблицей; при пустом результате — «Nothing found» на фоне; обновление по `refreshSignal`), `VideoListViewModel` (загрузка, `filterText` → `filteredFiles`: по имени A–Z, подстрока без учёта регистра; удаление с подтверждением), `VideoItemView` |
| `Scenes/Watchlist/` | `WatchlistView` (поле «User name or link» + Add; для ссылки подсказка «Will add: <имя>», на сервер уходит только имя — `WatchlistViewModel.userName(from:)`; сервер отвечает после проверки recorder'ом (5–8 с) — под полем «Checking <имя>…», поле не очищается, если в нём успели ввести другое; ошибка (в т.ч. «not found, not added») под полем, после любого действия список перечитывается, свайп Remove с подтверждением, автообновление статусов раз в 10 с), `WatchlistViewModel`, `WatchEntryView` (статус: 🔴 запись / 👁 слежение / 🚫 доступ запрещён / ❓ не найден / ⏳ ждёт recorder / ⏸ выключен + кнопка Enable) |
| `Scenes/VideoView/` | `VideoPlayerView`, `VideoPlayerViewModel` — `AVURLAsset` с заголовком `Authorization` через `AVURLAssetHTTPHeaderFieldsKey`, URL `http://host + hlsURL`, позиция сохраняется на паузе, сбрасывается в конце |
| `Scenes/Settings/` | `SettingsView`, `GlobalSettings` (оверлей производительности), `PerfomanceMonitor/` |
| `Controls/FSVideoPlayer/` | свой плеер: `FSPlayerController` (play/pause/seek/scrub, очередь seek), `FSVideoPlayerLayerView`, `FSVideoPlayerView`, `FSVideoScrubber` |
| `Services/` | `AuthService`, `VideoService`, `WatchlistService` (singletons `@MainActor`, `Result<_, Error>`; протоколы `@MainActor` — иначе Swift 6 не пропускает), `NetworkServiceError`, `KeychainTokenService` (токен), `UserDataStorageService` (UserDefaults: host, позиции видео), `SessionStorage` (`ObservableObject`: token, host, logout) |
| `Models/VideoItemModel.swift` | DTO `GET /videos` — должен совпадать с Go `MediaFile` в `services/api/internal/httpapi/videos.go` |
| `Models/WatchEntryModel.swift` | DTO `GET /watchlist` (Go `WatchEntry` в `watchlist.go`) и вычисляемый `status` |
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
| `surface` / `surfaceElevated` | deep900 / deep800 | карточки и строки, поля (в т.ч. фильтр), форма настроек / выделение |
| `separator` / `border` | deep700 / sky700 | разделители строк / рамка полей |
| `textPrimary` / `textSecondary` / `textTertiary` | deep50 / deep300 / deep500 | основной текст / метаданные, иконки полей, «Checking…», статус «Not found» / неактивное, «Nothing found», «No users yet», статус «Waiting for recorder» |
| `accent` | sky400 (также `AccentColor`) | текст и иконки интерактивных элементов, выбранная вкладка, статус «Watching» (4.8 на surface) |
| `accentFill` / `accentPressed` | sky500 / sky300 | дорожка переключателя / нажатое состояние |
| `primary` / `primaryPressed` / `onPrimary` | ocean700 / ocean800 / белый | заливка кнопок Login, Back to Login, Add, Enable |
| `playback` | orange400 | иконка play, прогресс и бегунок плеера, статус «Recording» |
| `destructive` / `destructiveFill` | orange400 / orange600 | удаление: текст / заливка свайпа под белым (5.4) |
| `warning` / `onWarning` | yellow200 / deep950 | тексты ошибок, значок ошибки в плеере, оверлей производительности, статусы «Disabled» и «Access denied» |
| `info` | sky400 | информационные сообщения |

Зелёного в палитре нет, поэтому роли `success` нет — при необходимости согласовать цвет.
Глобальные цвета UIKit (панель навигации) — в `FSPlayerApp.init`. Плеер чёрный под видео с белыми элементами.
Поля ввода — модификатор `themedField()` (системный `.roundedBorder` в тёмной теме заливает чёрным).

## Проверка экранов без сервера

Поднять `services/api` с отдельной `CBR_HOME` и тестовым видео (`ffmpeg -f lavfi -i testsrc2=… -f hls …` в `media/<имя>/`),
войти в симуляторе на `127.0.0.1:<порт>`. Для снимков удобен временный UI-тест в `CBRPlayerUITests/` с `XCTAttachment`
и выгрузкой через `xcrun xcresulttool export attachments`; после входа iOS показывает системный диалог «Сохранить пароль?» —
его закрывает `XCUIApplication(bundleIdentifier: "com.apple.springboard")`. Временный тест после проверки удалить.

- Состояния пользователей без запущенного recorder: добавить через `POST /watchlist`, затем прямо в базе
  `sqlite3 $CBR_HOME/data/cbr.db` — `UPDATE watchlist SET enabled=0, disabled_reason=…`, `INSERT INTO watch_state(user, room_status, recording, updated_at)` (время в unix ms).
- Приложение помнит `host:port` с прошлого входа — в тесте поля очищать перед вводом.
- Нажатие на вкладку, пока исчезает диалог пароля, теряется — повторять, пока не появится контент вкладки.
- Кнопка удаления в системном алерте красноватая — это цвет iOS, палитрой не задаётся.

## Соглашения

- MVVM: `ObservableObject` + `@Published`, view model'и `@MainActor`; DI через `environmentObject`.
- Сеть — `URLSession` async/await, plain HTTP на `host:port`, ошибки маппятся в `NetworkServiceError`.
