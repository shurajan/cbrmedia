//
//  WatchlistService.swift
//  CBRPlayer
//
//  Список пользователей для слежения и записи: GET/POST /watchlist, DELETE /watchlist/:user,
//  ссылка на живой стрим: GET /watchlist/:user/live.
//

import Foundation

@MainActor
protocol WatchlistServiceProtocol {
    func fetch(host: String, token: String) async -> Result<[WatchEntryModel], Error>
    /// Добавляет пользователя или снова включает выключенного.
    func add(user: String, host: String, token: String) async -> Result<Void, Error>
    func remove(user: String, host: String, token: String) async -> Result<Void, Error>
    /// Свежая ссылка на живой стрим; `.conflict`, если пользователь сейчас не public.
    func live(user: String, host: String, token: String) async -> Result<LiveStreamModel, Error>
}

@MainActor
final class WatchlistService: WatchlistServiceProtocol {
    static let shared = WatchlistService()

    private init() {}

    func fetch(host: String, token: String) async -> Result<[WatchEntryModel], Error> {
        switch await send("GET", path: "/watchlist", host: host, token: token) {
        case .success(let data):
            do {
                return .success(try JSONDecoder().decode([WatchEntryModel].self, from: data))
            } catch {
                return .failure(NetworkServiceError.decodingError(message: "Failed to decode watchlist: \(error.localizedDescription)"))
            }
        case .failure(let error):
            return .failure(error)
        }
    }

    func add(user: String, host: String, token: String) async -> Result<Void, Error> {
        guard let body = try? JSONEncoder().encode(["user": user]) else {
            return .failure(NetworkServiceError.networkError(message: "Failed to encode request"))
        }
        return await send("POST", path: "/watchlist", host: host, token: token, body: body).map { _ in () }
    }

    func remove(user: String, host: String, token: String) async -> Result<Void, Error> {
        guard let encoded = user.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) else {
            return .failure(NetworkServiceError.networkError(message: "Invalid user name: \(user)"))
        }
        return await send("DELETE", path: "/watchlist/\(encoded)", host: host, token: token).map { _ in () }
    }

    func live(user: String, host: String, token: String) async -> Result<LiveStreamModel, Error> {
        guard let encoded = user.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) else {
            return .failure(NetworkServiceError.networkError(message: "Invalid user name: \(user)"))
        }
        switch await send("GET", path: "/watchlist/\(encoded)/live", host: host, token: token) {
        case .success(let data):
            do {
                return .success(try JSONDecoder().decode(LiveStreamModel.self, from: data))
            } catch {
                return .failure(NetworkServiceError.decodingError(message: "Failed to decode live stream: \(error.localizedDescription)"))
            }
        case .failure(let error):
            return .failure(error)
        }
    }

    // MARK: - Private

    private struct ErrorResponse: Decodable {
        let error: String
    }

    /// Выполняет запрос и возвращает тело успешного (2xx) ответа. Для остальных
    /// статусов — ошибка с текстом из JSON `{"error": "..."}`, если он есть.
    private func send(_ method: String, path: String, host: String, token: String, body: Data? = nil) async -> Result<Data, Error> {
        guard !token.isEmpty else {
            return .failure(NetworkServiceError.invalidToken(message: "Authorization token is empty"))
        }
        guard let url = URL(string: "http://\(host)\(path)") else {
            return .failure(NetworkServiceError.networkError(message: "Invalid URL"))
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                return .failure(NetworkServiceError.invalidResponse(message: "Invalid response format"))
            }
            switch http.statusCode {
            case 200..<300:
                return .success(data)
            case 401:
                return .failure(NetworkServiceError.invalidToken(message: "Authorization failed: Invalid or expired token"))
            default:
                let message = (try? JSONDecoder().decode(ErrorResponse.self, from: data))?.error
                    ?? "HTTP \(http.statusCode)"
                if http.statusCode == 409 {
                    return .failure(NetworkServiceError.conflict(message: message))
                }
                return .failure(NetworkServiceError.serverError(message: message))
            }
        } catch {
            return .failure(NetworkServiceError.networkError(message: "Network request failed: \(error.localizedDescription)"))
        }
    }
}
