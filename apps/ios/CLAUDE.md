# CBRPlayer (iOS / iPadOS)

SwiftUI-клиент службы `services/api`: логин по паролю, главная с плитками разделов, экраны «Videos» (медиатека,
HLS-плеер с сохранением позиции) и «Watchlist» (кого recorder отслеживает и записывает, живой просмотр public-стримов).

- Xcode-проект `CBRPlayer.xcodeproj`, схема **CBRPlayer**, только iPad (`TARGETED_DEVICE_FAMILY = 2`), iOS 26.6, Swift 6, bundle `com.bralnin.cbrplayer`.
- Без внешних SPM-зависимостей. Info.plist генерируется (`GENERATE_INFOPLIST_FILE`, ключи `INFOPLIST_KEY_*` в pbxproj):
  отображаемое имя `cbr` (`CFBundleDisplayName`), категория `public.app-category.video`, всегда тёмная тема (`UIUserInterfaceStyle = Dark`).
- `MARKETING_VERSION` у всех таргетов = версия проекта из корневого `CLAUDE.md` (сейчас 1.3.1) — менять вместе с ней.
- Иконка — `Assets.xcassets/AppIcon.appiconset/CBR_AppIcon_1024.png` (один размер 1024, остальные Xcode генерирует).
- Группы — file-system synchronized (objectVersion 77): новый `.swift` в `CBRPlayer/` попадает в таргет автоматически, pbxproj править не нужно.
- Сборка из CLI: `xcodebuild -project CBRPlayer.xcodeproj -scheme CBRPlayer -destination 'platform=iOS Simulator,name=iPad Pro 13-inch (M5)' build`
  (нужен симулятор iOS ≥ 26.6).
- Тесты (Swift Testing): `CBRPlayerTests/VideoListFilterTests` — фильтр видео, `WatchlistTests` — модель `/watchlist`,
  `WatchlistViewModel` и `LivePlayerViewModel` на фейковом сервисе; `CBRPlayerUITests` пуст (шаблоны удалены), сюда кладутся временные UI-тесты для снимков. Deployment target 26.6 у проекта и всех таргетов —
  менять синхронно, иначе тесты не соберутся. Исторические имена `FS*` (от FSPlayer) сохранены.

## Структура `CBRPlayer/`

| Путь | Что |
|------|-----|
| `FSPlayerApp.swift` | `@main`, настройка UINavigationBar |
| `Scenes/FSPlayerView.swift` | корень: `NavigationStack` по `NavigationDestination {login, main, videos, watchlist}`, держит `SessionStorage`, `GlobalSettings` |
| `Scenes/Main/` | после входа — главная `MainView` (крупный заголовок «Home», строка «Connected to host», сетка 3 плиток в ряд, `maxWidth` 960; без `ScrollView` — в нём крупный заголовок в iOS 26 не рисуется): плитка `HomeTileView` — `NavigationLink(value:)` на дочерний экран; набор плиток фиксированный, новая появляется вместе с новой функцией. Кнопки: назад к входу (слева), настройки (справа). `HomeViewModel` при каждом показе (`onAppear`, в т.ч. после возврата с дочернего экрана) параллельно грузит `/videos` и `/watchlist` для сводки на плитках («48 videos · 112 GB», «5 users · ● 1 recording» цветом `recording`); ошибки не показывает — сводка остаётся прежней или пустой |
| `Scenes/LoginView/` | `LoginView`, `LoginViewModel` (host + пароль → `AuthService.login`), `HostAndPortInputView` |
| `Scenes/VideoList/` | `VideoListView` (дочерний экран: заголовок «Videos», системная «назад» → `Home`, своя кнопка «обновить» в `.toolbar`; поле фильтра над таблицей; при пустом результате — «Nothing found» на фоне), `VideoListViewModel` (загрузка — её ошибка `errorMessage` заменяет список экраном с «Back to Login»; `filterText` → `filteredFiles`: по имени A–Z, подстрока без учёта регистра; удаление с подтверждением — его ошибка `actionError` показывается над списком, список остаётся), `VideoItemView` |
| `Scenes/Watchlist/` | `WatchlistView` (дочерний экран с теми же заголовком/«назад»/«обновить»; поле «User name or link» + Add; если итоговое имя отличается от ввода (ссылка, заглавные буквы), подсказка «Will add: <имя>», на сервер уходит только имя в нижнем регистре — `WatchlistViewModel.userName(from:)`; сервер отвечает после проверки recorder'ом (5–8 с) — под полем «Checking <имя>…», поле не очищается, если в нём успели ввести другое; ошибка (в т.ч. «not found, not added») под полем, после любого действия список перечитывается, свайп Remove с подтверждением, автообновление статусов раз в 10 с; 401 (токен отозван входом с другого устройства) → `sessionExpired`: опрос останавливается, вместо списка ошибка и «Back to Login», как в Videos), `WatchlistViewModel`, `WatchEntryView` (статус: 🔴 запись / 👁 слежение / 🚫 доступ запрещён / ❓ не найден / ⏳ ждёт recorder / ⏸ выключен + кнопка Enable; у `isLive` (`roomStatus == "public"`) — кнопка play как в Videos → `liveEntry` → `fullScreenCover` с `LivePlayerView`) |
| `Scenes/LiveView/` | `LivePlayerView` (модально: «Connecting to <user>…», плеер, экран «<user> is not live now» / «Can't play…» с кнопкой закрыть), `LivePlayerViewModel` (`phase`: `GET /watchlist/:user/live` → `AVURLAsset` прямо на CDN **без** `Authorization` — токен api не уходит на CDN; 409 и конец эфира (`AVPlayerItemDidPlayToEndTime`) → not live, ошибка AVPlayer → can't play; позиция не сохраняется; ответ, пришедший после закрытия экрана (`Task.isCancelled`), игнорируется — иначе плеер с time observer некому остановить) |
| `Scenes/VideoView/` | `VideoPlayerView`, `VideoPlayerViewModel` — `AVURLAsset` с заголовком `Authorization` через `AVURLAssetHTTPHeaderFieldsKey`, URL `http://host + hlsURL`, позиция сохраняется на паузе и при закрытии плеера (`cleanup` — до остановки, KVO паузы туда уже не успевает), сбрасывается в конце |
| `Scenes/Settings/` | `SettingsView`, `GlobalSettings` (оверлей производительности), `PerfomanceMonitor/` |
| `Controls/FSVideoPlayer/` | свой плеер: `FSPlayerController` (play/pause/seek/scrub, очередь seek, `playLive` — play с живого края; time observer снимает `cleanup()` из `onDisappear`, страховка — `deinit`: AVPlayer с живым observer падает при освобождении), `FSVideoPlayerLayerView`, `FSVideoPlayerView` (с `live: LiveInfo` — режим живого стрима: имя по центру сверху, только пауза/play, внизу плашка LIVE и «Watching mm:ss» вместо времени и бегунка), `FSVideoScrubber` |
| `Services/` | `AuthService`, `VideoService`, `WatchlistService` (singletons `@MainActor`, `Result<_, Error>`; протоколы `@MainActor` — иначе Swift 6 не пропускает), `NetworkServiceError`, `KeychainTokenService` (токен), `UserDataStorageService` (UserDefaults: host, позиции видео), `SessionStorage` (`ObservableObject`: token, host, logout) |
| `Models/VideoItemModel.swift` | DTO `GET /videos` — должен совпадать с Go `MediaFile` в `services/api/internal/httpapi/videos.go` |
| `Models/WatchEntryModel.swift` | DTO `GET /watchlist` (Go `WatchEntry` в `watchlist.go`) и вычисляемые `status`, `isLive` |
| `Models/LiveStreamModel.swift` | DTO `GET /watchlist/:user/live` (Go `LiveStream` в `live.go`) |
| `Models/WatchAddResultModel.swift` | DTO ответа 201 на `POST /watchlist` (Go `checkNames` в `watchlist.go`): `check == "recorder_offline"` → `WatchlistViewModel.actionNotice` («Recorder is not running…», цвет `info`) под полем, пользователь при этом добавлен |
| `Helpers/Theme.swift` | палитра `Palette` и роли `Theme` (см. ниже), `Color(hex:)`, модификаторы `themedScreenBackground()`, `themedField()` |
| `Helpers/View+Extension.swift` | оверлей производительности |

## Оформление

Приложение всегда в тёмной теме (`INFOPLIST_KEY_UIUserInterfaceStyle = Dark`), системная светлая тема не учитывается.
В экранах — только роли `Theme`; `Palette` — сырьё для ролей. Без `.red`/`.green`/системных фонов.

**Палитра** (`Palette` в `Helpers/Theme.swift`): семь исходных цветов, у каждого тональная шкала 50…950, построенная в OKLCH
(тон сохраняется, светлота равномерна для глаза). Исходный цвет стоит в шкале без изменений:
`ocean700` = `#0c6a93`, `sky500` = `#2f85ac`, `deep900` = `#113844`, `orange400` = `#f47321`, `yellow200` = `#f0bd22`,
`green400` = `#34c26b` (воспроизведение), `red400` = `#ff6b6b` (удаление, запись) — привычные людям цвета для play и delete.

**Роли** (`Theme`), контраст по WCAG — текст ≥ 4.5, иконки ≥ 3:

| Роль | Цвет | Где |
|---|---|---|
| `background` | deep950 | фон экранов, панель навигации, подложка кнопки play и иконки плитки |
| `surface` / `surfaceElevated` | deep900 / deep800 | карточки, строки, плитки главной, поля (в т.ч. фильтр), форма настроек / выделение |
| `separator` / `border` | deep700 / sky700 | разделители строк / рамка полей |
| `textPrimary` / `textSecondary` / `textTertiary` | deep50 / deep300 / deep500 | основной текст / метаданные, иконки полей, «Checking…», статус «Not found» / неактивное, «Nothing found», «No users yet», статус «Waiting for recorder», сводка и шеврон плитки, «Connected to …» |
| `accent` | sky400 (также `AccentColor`) | текст и иконки интерактивных элементов, иконка плитки, статус «Watching» (4.8 на surface) |
| `accentFill` / `accentPressed` | sky500 / sky300 | дорожка переключателя / нажатое состояние |
| `primary` / `primaryPressed` / `onPrimary` | ocean700 / ocean800 / белый | заливка кнопок Login, Back to Login, Add, Enable |
| `play` | green400 | иконка play в списке видео и у public-строк Watchlist (7.4 на фоне, 5.5 на surface) |
| `playback` | orange400 | прогресс и бегунок плеера |
| `destructive` / `destructiveFill` | red400 / red600 | удаление: текст и иконки (4.6 на surface) / заливка свайпа под белым (5.2) |
| `recording` | red400 | статус «Recording» в Watchlist, точка и «N recording» на плитке главной, точка плашки LIVE в живом плеере |
| `warning` / `onWarning` | yellow200 / deep950 | тексты ошибок, значок ошибки в плеере, оверлей производительности, статусы «Disabled» и «Access denied» |
| `info` | sky400 | информационные сообщения |

Глобальные цвета UIKit (панель навигации) — в `FSPlayerApp.init`. Плеер чёрный под видео с белыми элементами.
Поля ввода — модификатор `themedField()` (системный `.roundedBorder` в тёмной теме заливает чёрным).

## Проверка экранов без сервера

Поднять `services/api` с отдельной `CBR_HOME` и тестовым видео (`ffmpeg -f lavfi -i testsrc2=… -f hls …` в `media/<имя>/`),
войти в симуляторе на `127.0.0.1:<порт>`. Для снимков удобен временный UI-тест в `CBRPlayerUITests/` с `XCTAttachment`
и выгрузкой через `xcrun xcresulttool export attachments`; после входа iOS показывает системный диалог «Сохранить пароль?» —
его закрывает `XCUIApplication(bundleIdentifier: "com.apple.springboard")`. Временный тест после проверки удалить.

- Состояния пользователей без запущенного recorder: добавить через `POST /watchlist`, затем прямо в базе
  `sqlite3 $CBR_HOME/data/cbr.db` — `UPDATE watchlist SET enabled=0, disabled_reason=…`, `INSERT INTO watch_state(user, room_status, recording, updated_at)` (время в unix ms).
- Живой плеер без настоящего стрима: `ffmpeg -re -f lavfi -i testsrc2=… -f hls -hls_list_size 6 -hls_flags delete_segments`
  в папку, раздать её и JSON status API (`<папка>/<user>/index.html` с `room_status`/`hls_source`) через
  `python3 -m http.server`, в `api.json` — `status_base_url` на него; `hls_source` на `127.0.0.1` ATS пропускает.
- Приложение помнит `host:port` с прошлого входа — в тесте поля очищать перед вводом.
- Нажатие на плитку, пока исчезает диалог пароля, теряется — повторять, пока не появится дочерний экран.
- Кнопка удаления в системном алерте красноватая — это цвет iOS, палитрой не задаётся.

## Соглашения

- MVVM: `ObservableObject` + `@Published`, view model'и `@MainActor`; DI через `environmentObject`.
- Сеть — `URLSession` async/await, plain HTTP на `host:port`, ошибки маппятся в `NetworkServiceError`.
