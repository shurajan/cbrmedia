# design — макет интерфейса cbr

Эталон дизайна клиентов (iOS реализован, Android и web — по этому же макету). Источник истины для
цветов — `apps/ios/CBRPlayer/Helpers/Theme.swift`; `tokens.json` сгенерирован из него и должен
совпадать (при смене палитры в Swift перегенерировать, см. ниже).

| Файл | Что |
|------|-----|
| `tokens.json` | палитра (7 шкал 50…950, OKLCH) и роли цветов с назначением; радиусы и отступы плиток |
| `canvas/*.dc.html` | артборды iPad 1194×834: `Login`, `Main` (главная с плитками), `Videos`, `Watchlist`, `Palette` (новые шкалы green/red) |
| `canvas/canvas.json` | раскладка артбордов на холсте и заметки о навигации |

Холст с кликабельным прототипом: https://claude.ai/artifact/5LhFDWrqaSGvL25sc45g15 (приватный). Артборды — обычный HTML:
открываются в браузере и без холста (строка `support.js` в `<head>` нужна только холсту, её отсутствие не мешает).
Ссылки между артбордами (`href="Videos.dc.html"`) работают как переходы прототипа.

## Экраны и навигация

```
Login ──вход──▶ Home (плитки) ──плитка──▶ Videos
                   │  ▲                     │ назад
                   │  └─────────────────────┘
                   └───плитка──▶ Watchlist ──назад──▶ Home
```

- **Login** — крупный заголовок, поля host / port / password, кнопка `primary`.
- **Home** — крупный заголовок «Home», строка «Connected to host:port» (`textTertiary`), сетка плиток
  3 в ряд (на узкой ширине — 2), `contentMaxWidth` 960. Слева кнопка назад к входу, справа настройки.
  Набор плиток фиксированный: новая появляется вместе с новой функцией.
- **Плитка** — поверхность `surface`, радиус 20, отступ 24, высота ≥ 232: иконка `accent` 28 px в круге
  `background` 60 px, шеврон `textTertiary` справа сверху; название (title2, semibold, `textPrimary`),
  описание (subheadline, `textSecondary`), сводка (footnote, `textTertiary`): «48 videos · 112 GB»,
  «5 users · ● 1 recording» — идущая запись точкой и текстом цвета `recording`.
- **Дочерние экраны** (Videos, Watchlist) — заголовок по центру, системная кнопка назад на Home,
  справа только действия экрана (обновить). Списки — строки `surface` радиус 12, разделитель `separator`.
  Свайп влево открывает действие удаления: заливка `destructiveFill`, белый текст и корзина.
- **Videos** — поле фильтра, строка: имя (headline), метаданные (caption2, `textSecondary`), кнопка play:
  круг `background` 52 px с треугольником `play` (зелёный).
- **Watchlist** — поле «User name or link» + кнопка Add (`primary`); строка: иконка статуса 22 px, имя, статус,
  «Added … via …». Цвета статусов: Recording — `recording`, Watching — `accent`, Waiting — `textTertiary`,
  Not found — `textSecondary`, Disabled и Access denied — `warning`; у выключенного кнопка Enable (`primary`).

## Цвета

Тёмная тема, светлой нет. В интерфейсе используются только роли (`roles` в `tokens.json`), шкалы — сырьё.
Контраст по WCAG: текст ≥ 4.5, иконки и крупные элементы ≥ 3 (значения — в описании ролей в `Theme.swift`).
Привычные людям цвета: play — зелёный (`play` = green400), удаление и идущая запись — красный
(`destructive` = red400, свайп `destructiveFill` = red600, `recording` = red400). Оранжевый `playback` —
только прогресс и бегунок плеера; жёлтый `warning` — ошибки и предупреждения.

## Перегенерация `tokens.json`

После правки `Theme.swift` (в корне репозитория):

```bash
python3 design/gen_tokens.py
```
