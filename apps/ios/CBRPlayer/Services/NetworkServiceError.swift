//
//  ServiceError.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 20.05.2025.
//
import Foundation

enum NetworkServiceError: LocalizedError {
    case invalidToken(message: String)
    case networkError(message: String)
    case invalidResponse(message: String)
    case decodingError(message: String)
    case invalidPassword(message: String)
    case serverError(message: String)
    /// 409: состояние на сервере не позволяет действие (например, стрим уже не public).
    case conflict(message: String)

    var errorDescription: String? {
        switch self {
        case .invalidToken(let message),
                .networkError(let message),
                .invalidResponse(let message),
                .decodingError(let message),
                .invalidPassword(let message),
                .serverError(let message),
                .conflict(let message):
            return message
        }
    }
}
