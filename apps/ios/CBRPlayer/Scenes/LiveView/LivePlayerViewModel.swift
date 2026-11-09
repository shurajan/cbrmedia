//
//  LivePlayerViewModel.swift
//  CBRPlayer
//
//  Живой стрим пользователя из Watchlist: свежая ссылка от api (GET /watchlist/:user/live),
//  затем AVPlayer прямо на CDN. Позиция не сохраняется, перемотки нет.
//

import AVFoundation
import Combine

@MainActor
final class LivePlayerViewModel: ObservableObject {

    enum Phase: Equatable {
        /// api запрашивает ссылку у status API.
        case connecting
        case playing
        /// Смотреть нельзя: эфир кончился, стал не public или ошибка.
        case unavailable(title: String, detail: String)
    }

    @Published private(set) var phase: Phase = .connecting
    @Published private(set) var playerController: FSPlayerController?
    /// Когда начали смотреть — для «Watching mm:ss».
    private(set) var joinedAt = Date()

    let user: String
    private let service: WatchlistServiceProtocol
    private var cancellables = Set<AnyCancellable>()

    init(user: String, service: WatchlistServiceProtocol? = nil) {
        self.user = user
        self.service = service ?? WatchlistService.shared
    }

    func connect(host: String?, token: String?) async {
        guard let host, let token else {
            phase = cantPlay("Missing host or token")
            return
        }
        phase = .connecting
        switch await service.live(user: user, host: host, token: token) {
        case .success(let stream):
            start(stream)
        case .failure(NetworkServiceError.conflict):
            phase = notLive
        case .failure(let error):
            phase = cantPlay(error.localizedDescription)
        }
    }

    func stop() {
        cancellables.removeAll()
        playerController?.player.pause()
        playerController?.player.replaceCurrentItem(with: nil)
        playerController = nil
    }

    // MARK: - Private

    private func start(_ stream: LiveStreamModel) {
        guard let url = URL(string: stream.hlsURL) else {
            phase = cantPlay("Invalid stream URL")
            return
        }
        // Без заголовка Authorization: токен api не должен уходить на CDN.
        let item = AVPlayerItem(asset: AVURLAsset(url: url))
        let player = AVPlayer(playerItem: item)
        player.automaticallyWaitsToMinimizeStalling = true
        player.allowsExternalPlayback = false
        let controller = FSPlayerController(player: player)

        controller.$errorMessage
            .compactMap { $0 }
            .receive(on: DispatchQueue.main)
            .sink { [weak self] message in
                guard let self else { return }
                self.stop()
                self.phase = self.cantPlay(message)
            }
            .store(in: &cancellables)

        // Эфир закончился: CDN дописал #EXT-X-ENDLIST.
        NotificationCenter.default.publisher(for: .AVPlayerItemDidPlayToEndTime, object: item)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                guard let self else { return }
                self.stop()
                self.phase = self.notLive
            }
            .store(in: &cancellables)

        joinedAt = Date()
        playerController = controller
        phase = .playing
    }

    private var notLive: Phase {
        .unavailable(title: "\(user) is not live now",
                     detail: "The stream ended or became private. Recordings appear in Videos.")
    }

    private func cantPlay(_ message: String) -> Phase {
        .unavailable(title: "Can't play \(user)'s stream", detail: message)
    }
}
