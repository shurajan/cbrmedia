import Foundation
import Testing
@testable import CBRPlayer

/// Фейковый сервис: хранит список в памяти, умеет падать по запросу.
@MainActor
final class FakeWatchlistService: WatchlistServiceProtocol {
    var entries: [WatchEntryModel] = []
    var failNext: String?
    private(set) var added: [String] = []
    private(set) var removed: [String] = []

    private func failure() -> NetworkServiceError? {
        defer { failNext = nil }
        return failNext.map { NetworkServiceError.serverError(message: $0) }
    }

    func fetch(host: String, token: String) async -> Result<[WatchEntryModel], Error> {
        if let error = failure() { return .failure(error) }
        return .success(entries)
    }

    func add(user: String, host: String, token: String) async -> Result<Void, Error> {
        if let error = failure() { return .failure(error) }
        added.append(user)
        entries.removeAll { $0.user == user }
        entries.append(.make(user))
        return .success(())
    }

    func remove(user: String, host: String, token: String) async -> Result<Void, Error> {
        if let error = failure() { return .failure(error) }
        removed.append(user)
        entries.removeAll { $0.user == user }
        return .success(())
    }
}

extension WatchEntryModel {
    static func make(_ user: String, enabled: Bool = true, reason: String? = nil,
                     room: String? = nil, recording: Bool = false, updatedAt: String? = nil) -> WatchEntryModel {
        WatchEntryModel(user: user, addedAt: "2026-11-05T10:00:00Z", addedBy: "api", enabled: enabled,
                        disabledReason: reason, roomStatus: room, recording: recording, updatedAt: updatedAt)
    }
}

struct WatchEntryModelTests {

    @Test func decodesAPIResponse() throws {
        let json = """
        [{"user":"alice","addedAt":"2026-11-05T10:00:00Z","addedBy":"telegram","enabled":true,
          "roomStatus":"public","recording":true,"updatedAt":"2026-11-05T10:01:00Z"},
         {"user":"bob","addedAt":"2026-11-05T10:00:00Z","addedBy":"api","enabled":false,
          "disabledReason":"status API returned HTTP 404","recording":false}]
        """
        let entries = try JSONDecoder().decode([WatchEntryModel].self, from: Data(json.utf8))
        #expect(entries.map(\.user) == ["alice", "bob"])
        #expect(entries[0].status == .recording(roomStatus: "public"))
        #expect(entries[1].status == .disabled(reason: "status API returned HTTP 404"))
        #expect(entries[0].addedDate != nil)
    }

    @Test func statusMapping() {
        #expect(WatchEntryModel.make("a").status == .waiting)
        #expect(WatchEntryModel.make("a", room: "offline", updatedAt: "x").status == .watching(roomStatus: "offline"))
        // Выключенный важнее остального состояния.
        #expect(WatchEntryModel.make("a", enabled: false, reason: "r", recording: true).status == .disabled(reason: "r"))
    }
}

@MainActor
struct WatchlistViewModelTests {

    @Test func addTrimsNameAndClearsField() async {
        let service = FakeWatchlistService()
        let vm = WatchlistViewModel(service: service)
        vm.newUser = "  alice \n"
        #expect(vm.canAdd)

        await vm.addNewUser(host: "h", token: "t")

        #expect(service.added == ["alice"])
        #expect(vm.newUser.isEmpty)
        #expect(vm.entries.map(\.user) == ["alice"])
        #expect(vm.actionError == nil)
    }

    @Test func addFailureKeepsInputAndShowsError() async {
        let service = FakeWatchlistService()
        service.failNext = "user already exists"
        let vm = WatchlistViewModel(service: service)
        vm.newUser = "alice"

        await vm.addNewUser(host: "h", token: "t")

        #expect(vm.newUser == "alice")
        #expect(vm.actionError == "user already exists")
    }

    @Test func blankInputCannotBeAdded() {
        let vm = WatchlistViewModel(service: FakeWatchlistService())
        vm.newUser = "   "
        #expect(!vm.canAdd)
    }

    @Test func deleteAndEnable() async {
        let service = FakeWatchlistService()
        service.entries = [.make("alice"), .make("bob", enabled: false, reason: "HTTP 404")]
        let vm = WatchlistViewModel(service: service)
        await vm.load(host: "h", token: "t")

        await vm.delete(vm.entries[0], host: "h", token: "t")
        #expect(service.removed == ["alice"])
        #expect(vm.entries.map(\.user) == ["bob"])

        await vm.enable(vm.entries[0], host: "h", token: "t")
        #expect(service.added == ["bob"])
        #expect(vm.entries.first?.enabled == true)
    }

    @Test func silentReloadFailureKeepsList() async {
        let service = FakeWatchlistService()
        service.entries = [.make("alice")]
        let vm = WatchlistViewModel(service: service)
        await vm.load(host: "h", token: "t")

        service.failNext = "network down"
        await vm.load(host: "h", token: "t", silent: true)
        #expect(vm.entries.map(\.user) == ["alice"])
        #expect(vm.loadError == nil)

        service.failNext = "network down"
        await vm.load(host: "h", token: "t")
        #expect(vm.loadError == "network down")
    }
}
