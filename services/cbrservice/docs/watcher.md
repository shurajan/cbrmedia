# Watcher (component of cbrservice)

`Watcher` is a Go application that monitors live streams and records them automatically. A Telegram bot is used to manage the list of watched users: each user is polled via a status API, and as soon as a stream goes live, the application records it natively as an HLS stream — no ffmpeg required. The watch list is persisted in an embedded database and restored on restart.

## Legal Notice

This software is provided for recording streams that you own or that you are expressly authorized to record. You are solely responsible for ensuring that your use of this software complies with all applicable copyright and digital rights legislation — including, where applicable, the Digital Millennium Copyright Act (DMCA), the EU Directive on Copyright in the Digital Single Market, and equivalent laws in your jurisdiction — as well as the terms of service of any platform from which content is accessed. Do not use this software to record, store, or distribute content without the consent of the rights holder. The authors and contributors accept no liability for any unauthorized or unlawful use.

## Features

- **Telegram bot control** — add and remove watched users from chat; access is restricted to a single allowed Telegram user ID.
- **Automatic recording** — when a watched user goes live, the stream is downloaded immediately.
- **Native HLS recorder** — resolves the media playlist (picking the highest-bandwidth variant from a master playlist), downloads segments continuously, decrypts AES-128 encrypted content, and handles fMP4/CMAF streams including init segments (a part never contains fragments without their init segment; a changed init starts a new part). A recording ends on `#EXT-X-ENDLIST`, when the playlist returns 4xx, or when no new segment could be written for 1 minute (or 6× the target duration, whichever is longer) — this catches CDNs that keep serving a frozen playlist after the broadcast ends.
- **Rotating output** — recordings are written to the `video/` directory as part files, rotated after every hour of recorded content, so each part is independently playable.
- **Persistent watch list** — users are stored in a [bbolt](https://github.com/etcd-io/bbolt) database and re-registered automatically on startup.
- **Proxy support**: both the poller and the Telegram bot can work through an HTTP proxy.
- **Error notifications** — failures (for example, a user that no longer exists) are reported back to you via the Telegram bot.

## Telegram commands

| Command | Description |
|---|---|
| `/add <user>` | Add a user to the watch list and start watching immediately |
| `/del <user>` | Remove a user; without an argument, shows an inline menu of users to delete |
| `/list` | List all watched users |

## Installation

Watcher is now part of `cbrservice`, see [../README.md](../README.md) for build, run and deploy instructions.

Recordings are saved into `~/.cbr/recordings/` (created automatically) as `<user>_<date>_part<N>.ts` or `.mp4`, depending on the detected stream format. Existing files are never overwritten: if a recording restarts within the same minute (same name), it continues with the next free part number.

## Configuration

The application reads `~/.cbr/config/config.json` (or `$CBR_HOME/config/config.json`). A template is in [`deploy/config.example.json`](../deploy/config.example.json).

A relative `storage.db` path is resolved against `~/.cbr/data/`.

### Example `config.json`

```json
{
  "base_url": "https://example.com/api/streamcontext",
  "storage": {
    "db": "data.db"
  },
  "url_poller": {
    "proxy": "http://127.0.0.1:8080"
  },
  "telegram": {
    "bot_token": "<your-bot-token>",
    "allowed_user_id": 123456789,
    "proxy": "http://127.0.0.1:8080"
  }
}
```

### Configuration parameters

- `base_url`: Status API base URL. Each watched user is polled at `<base_url>/<user>/`; the response is expected to contain the room status and, when live, an HLS source URL.
- `storage`:
  - `db`: Path to the bbolt database file holding the watch list (relative to `~/.cbr/data/`).
- `url_poller`:
  - `proxy` (optional): HTTP proxy URL used for status polling and stream downloading.
  - TLS certificate verification is **disabled** for status polling and stream downloading (a warning is logged at startup).
- `telegram`:
  - `bot_token`: Telegram bot token obtained from [@BotFather](https://t.me/BotFather).
  - `allowed_user_id`: Telegram user ID allowed to control the bot; commands from anyone else are denied.
  - `proxy` (optional): HTTP proxy URL used for the Telegram API connection.
- `http` (optional):
  - `addr`: listen address of the player HTTP API, default `:8000`.

The config is validated at startup: missing `base_url`, `telegram.bot_token`, `telegram.allowed_user_id` or an invalid proxy URL stop the service with a clear error.

> `config.json` contains your bot token. It lives in `~/.cbr/config/`, outside the repository, and is created with `chmod 600`.

## Polling behavior

The polling interval adapts to the room status: 15 seconds when the stream is public, 1 minute when the user is online but not publicly streaming (private, away, hidden, password protected), and 15 minutes when offline. If the status API returns a 4xx error for a user, that user's watcher is stopped until the next restart and a notification is sent via Telegram; on 5xx or network errors the poller backs off and retries.

## License

This project is distributed under the MIT license.
